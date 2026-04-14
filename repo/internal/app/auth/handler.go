// Package auth implements login, logout, session retrieval, and password change.
package auth

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"

	"portal/internal/app/common"
	appconfig "portal/internal/app/config"
	"portal/internal/app/mfa"
	"portal/internal/app/sessions"
	"portal/internal/app/users"
	"portal/internal/platform/logging"
)

// Handler exposes auth-related HTTP endpoints.
type Handler struct {
	users    *users.Store
	sessions *sessions.Store
	mfa      *mfa.Store
	config   *appconfig.Store
	log      *logging.Logger
}

// NewHandler constructs a Handler with its dependencies.
func NewHandler(u *users.Store, s *sessions.Store, m *mfa.Store, cfg *appconfig.Store, log *logging.Logger) *Handler {
	return &Handler{users: u, sessions: s, mfa: m, config: cfg, log: log}
}

// ── Request / response types ─────────────────────────────────────────────────

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	RequiresMFA       bool         `json:"requires_mfa"`
	CompatibilityMode string       `json:"compatibility_mode"`
	User              *userPayload `json:"user,omitempty"`
}

type userPayload struct {
	ID                 string   `json:"id"`
	Username           string   `json:"username"`
	DisplayName        string   `json:"display_name"`
	Roles              []string `json:"roles"`
	Permissions        []string `json:"permissions"`
	ForcePasswordReset bool     `json:"force_password_reset"`
	MFAEnrolled        bool     `json:"mfa_enrolled"`
	MFAVerified        bool     `json:"mfa_verified"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// Login authenticates a user with username + password and issues a session cookie.
// POST /api/v1/auth/login
func (h *Handler) Login(c echo.Context) error {
	var req loginRequest
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		return common.BadRequest(c, "validation.required", "Username and password are required")
	}

	user, err := h.users.GetByUsername(c.Request().Context(), req.Username)
	if err != nil {
		// Perform a dummy bcrypt comparison to make timing indistinguishable
		// from a real failure — prevents username enumeration.
		_ = bcrypt.CompareHashAndPassword(
			[]byte("$2a$12$aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			[]byte(req.Password),
		)
		return common.Unauthorized(c, "Invalid credentials")
	}

	if !user.IsActive {
		return common.Unauthorized(c, "Account is disabled")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		// Do NOT log the username — doing so aids username enumeration and
		// increases sensitive-data surface in log aggregators. Log only the
		// IP and a non-identifying event code.
		h.log.Info("login failed: wrong password", map[string]any{"ip": c.RealIP()})
		return common.Unauthorized(c, "Invalid credentials")
	}

	// Evaluate client version compatibility.
	clientVersion := c.Request().Header.Get("X-Client-Version")
	compatibilityMode := "full"
	if h.config != nil {
		rule, err := h.config.EvaluateClientVersion(c.Request().Context(), clientVersion)
		if err == nil && rule != nil {
			switch rule.Action {
			case "block":
				return common.ErrorResponse(c, http.StatusForbidden, "compatibility.blocked", rule.Message)
			case "read_only":
				compatibilityMode = "read_only"
			case "warn":
				compatibilityMode = "warn"
			}
		}
	}

	// Check if user has MFA enrolled.
	mfaEnrolled := false
	if h.mfa != nil {
		enrolled, _ := h.mfa.IsEnrolled(c.Request().Context(), user.ID)
		mfaEnrolled = enrolled
	}

	// Create server-side session and set HttpOnly cookie.
	// mfa_verified starts as false; the MFA challenge endpoint sets it to true.
	token, err := h.sessions.Create(
		c.Request().Context(),
		user.ID, clientVersion,
		c.RealIP(), c.Request().UserAgent(),
		false,
	)
	if err != nil {
		h.log.Error("create session", map[string]any{"err": err.Error()})
		return common.Internal(c)
	}

	// Use the Store-bound writer so cookie MaxAge mirrors the configured
	// absolute timeout (session.max_timeout_seconds) instead of the
	// hard-coded package default.
	h.sessions.WriteCookie(c, token)
	_ = h.users.UpdateLastLogin(c.Request().Context(), user.ID)

	h.log.Info("login success", map[string]any{"user_id": user.ID})

	// If MFA is enrolled, return partial session — do not return user data.
	if mfaEnrolled {
		return c.JSON(http.StatusOK, loginResponse{
			RequiresMFA:       true,
			CompatibilityMode: compatibilityMode,
			User:              nil,
		})
	}

	uw, err := h.users.GetWithRoles(c.Request().Context(), user.ID)
	if err != nil {
		h.log.Error("get user roles", map[string]any{"err": err.Error()})
		return common.Internal(c)
	}

	return c.JSON(http.StatusOK, loginResponse{
		RequiresMFA:       false,
		CompatibilityMode: compatibilityMode,
		User: &userPayload{
			ID:                 uw.ID,
			Username:           uw.Username,
			DisplayName:        uw.DisplayName,
			Roles:              nonNilSlice(uw.Roles),
			Permissions:        nonNilSlice(uw.Permissions),
			ForcePasswordReset: uw.ForcePasswordReset,
			MFAEnrolled:        mfaEnrolled,
			MFAVerified:        false,
		},
	})
}

// Logout invalidates the current session and clears the cookie.
// POST /api/v1/auth/logout
func (h *Handler) Logout(c echo.Context) error {
	token := sessions.TokenFromRequest(c)
	if token != "" {
		if err := h.sessions.Invalidate(c.Request().Context(), token); err != nil {
			h.log.Warn("logout invalidate error", map[string]any{"err": err.Error()})
		}
	}
	sessions.ClearCookie(c)
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// GetSession returns the currently authenticated user's profile.
// GET /api/v1/session  — requires RequireAuth middleware.
func (h *Handler) GetSession(c echo.Context) error {
	userID, ok := c.Get("user_id").(string)
	if !ok || userID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}

	uw, err := h.users.GetWithRoles(c.Request().Context(), userID)
	if err != nil {
		return common.Unauthorized(c, "Session invalid")
	}

	// Retrieve MFA state from session.
	mfaEnrolled := false
	if h.mfa != nil {
		enrolled, _ := h.mfa.IsEnrolled(c.Request().Context(), userID)
		mfaEnrolled = enrolled
	}
	mfaVerified := false
	if v, ok := c.Get("mfa_verified").(bool); ok {
		mfaVerified = v
	}

	// Evaluate client version compatibility.
	clientVersion := c.Request().Header.Get("X-Client-Version")
	compatibilityMode := "full"
	if h.config != nil {
		rule, err := h.config.EvaluateClientVersion(c.Request().Context(), clientVersion)
		if err == nil && rule != nil {
			switch rule.Action {
			case "block":
				compatibilityMode = "blocked"
			case "read_only":
				compatibilityMode = "read_only"
			case "warn":
				compatibilityMode = "warn"
			}
		}
	}

	return c.JSON(http.StatusOK, map[string]any{
		"user": &userPayload{
			ID:                 uw.ID,
			Username:           uw.Username,
			DisplayName:        uw.DisplayName,
			Roles:              nonNilSlice(uw.Roles),
			Permissions:        nonNilSlice(uw.Permissions),
			ForcePasswordReset: uw.ForcePasswordReset,
			MFAEnrolled:        mfaEnrolled,
			MFAVerified:        mfaVerified,
		},
		"compatibility_mode": compatibilityMode,
	})
}

// ChangePassword handles both bootstrap password rotation and regular changes.
// POST /api/v1/auth/password/change  — requires RequireAuth middleware.
func (h *Handler) ChangePassword(c echo.Context) error {
	userID, ok := c.Get("user_id").(string)
	if !ok || userID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}

	var req changePasswordRequest
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	if req.NewPassword == "" {
		return common.BadRequest(c, "validation.required", "New password is required")
	}
	if len(req.NewPassword) < 8 {
		return common.BadRequest(c, "validation.password_too_short", "Password must be at least 8 characters")
	}

	user, err := h.users.GetWithRoles(c.Request().Context(), userID)
	if err != nil {
		return common.Internal(c)
	}

	// Bootstrap rotation (force_password_reset=true): skip current-password check.
	// Regular change: current password must be supplied and verified.
	if !user.ForcePasswordReset {
		if req.CurrentPassword == "" {
			return common.BadRequest(c, "validation.required", "Current password is required")
		}
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
			return common.Unauthorized(c, "Current password is incorrect")
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), 12)
	if err != nil {
		h.log.Error("bcrypt generate", map[string]any{"err": err.Error()})
		return common.Internal(c)
	}

	if err := h.users.UpdatePasswordHash(c.Request().Context(), userID, string(hash)); err != nil {
		h.log.Error("update password hash", map[string]any{"err": err.Error()})
		return common.Internal(c)
	}

	h.log.Info("password changed", map[string]any{"user_id": userID})
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// nonNilSlice returns an empty slice instead of nil so JSON encodes [] not null.
func nonNilSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
