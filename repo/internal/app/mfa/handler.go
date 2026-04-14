package mfa

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"portal/internal/app/common"
	"portal/internal/app/sessions"
	"portal/internal/app/users"
	"portal/internal/platform/logging"
)

// Handler exposes MFA HTTP endpoints.
type Handler struct {
	store    *Store
	sessions *sessions.Store
	users    *users.Store
	log      *logging.Logger
}

// NewHandler constructs a Handler with its dependencies.
func NewHandler(store *Store, s *sessions.Store, u *users.Store, log *logging.Logger) *Handler {
	return &Handler{store: store, sessions: s, users: u, log: log}
}

// StartEnrollment begins TOTP setup. Returns QR URI and secret for display.
// POST /api/v1/mfa/enroll/start
func (h *Handler) StartEnrollment(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}

	u, err := h.users.GetWithRoles(c.Request().Context(), userID)
	if err != nil {
		return common.Internal(c)
	}

	enrollment, err := h.store.StartEnrollment(c.Request().Context(), userID, u.Username)
	if err != nil {
		h.log.Error("start mfa enrollment", map[string]any{"err": err.Error()})
		return common.Internal(c)
	}

	return c.JSON(http.StatusOK, map[string]string{
		"provisioning_uri": enrollment.ProvisioningURI,
		"secret":           enrollment.SecretPlaintext, // shown once only
	})
}

// ConfirmEnrollment verifies the setup code and completes enrollment.
// POST /api/v1/mfa/enroll/confirm
func (h *Handler) ConfirmEnrollment(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := c.Bind(&req); err != nil || req.Code == "" {
		return common.BadRequest(c, "validation.required", "TOTP code is required")
	}

	if err := h.store.ConfirmEnrollment(c.Request().Context(), userID, req.Code); err != nil {
		return common.BadRequest(c, "mfa.invalid_code", "Invalid TOTP code")
	}

	// Generate and return recovery codes (shown once).
	codes, err := h.store.GenerateRecoveryCodes(c.Request().Context(), userID)
	if err != nil {
		h.log.Error("generate recovery codes", map[string]any{"err": err.Error()})
		return common.Internal(c)
	}

	h.log.Info("mfa enrolled", map[string]any{"user_id": userID})
	return c.JSON(http.StatusOK, map[string]any{
		"enrolled":       true,
		"recovery_codes": codes, // shown once only
	})
}

// Verify handles the MFA challenge step (called after password login when mfa_required=true).
// POST /api/v1/mfa/verify
func (h *Handler) Verify(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := c.Bind(&req); err != nil || req.Code == "" {
		return common.BadRequest(c, "validation.required", "TOTP code is required")
	}

	if err := h.store.Verify(c.Request().Context(), userID, req.Code); err != nil {
		h.log.Info("mfa verify failed", map[string]any{"user_id": userID})
		return common.BadRequest(c, "mfa.invalid_code", "Invalid or expired TOTP code")
	}

	// Mark session as MFA-verified.
	sessionID, _ := c.Get("session_id").(string)
	if sessionID != "" {
		if err := h.sessions.SetMFAVerified(c.Request().Context(), sessionID); err != nil {
			h.log.Warn("set mfa verified", map[string]any{"err": err.Error()})
		}
	}

	h.log.Info("mfa verified", map[string]any{"user_id": userID})
	return c.JSON(http.StatusOK, map[string]bool{"mfa_verified": true})
}

// VerifyRecovery lets a user authenticate using a backup recovery code.
// POST /api/v1/mfa/recovery
func (h *Handler) VerifyRecovery(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := c.Bind(&req); err != nil || req.Code == "" {
		return common.BadRequest(c, "validation.required", "Recovery code is required")
	}

	if err := h.store.UseRecoveryCode(c.Request().Context(), userID, req.Code); err != nil {
		return common.BadRequest(c, "mfa.invalid_recovery_code", "Invalid or already used recovery code")
	}

	// Mark session as MFA-verified.
	sessionID, _ := c.Get("session_id").(string)
	if sessionID != "" {
		if err := h.sessions.SetMFAVerified(c.Request().Context(), sessionID); err != nil {
			h.log.Warn("set mfa verified via recovery", map[string]any{"err": err.Error()})
		}
	}

	h.log.Info("mfa recovery code used", map[string]any{"user_id": userID})
	return c.JSON(http.StatusOK, map[string]bool{"mfa_verified": true})
}
