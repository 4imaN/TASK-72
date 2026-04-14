package users

import (
	"context"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"portal/internal/app/common"
)

// auditRecorder is a minimal interface for recording reveal events.
type auditRecorder interface {
	RecordReveal(ctx context.Context, actorID, targetUserID, fieldName, reason, ipAddress string)
}

// AdminHandler exposes admin-only user management endpoints.
type AdminHandler struct {
	store      *Store
	auditStore auditRecorder
}

// NewAdminHandler constructs an AdminHandler.
func NewAdminHandler(store *Store) *AdminHandler {
	return &AdminHandler{store: store}
}

// NewAdminHandlerWithAudit constructs an AdminHandler with audit recording support.
func NewAdminHandlerWithAudit(store *Store, auditStore auditRecorder) *AdminHandler {
	return &AdminHandler{store: store, auditStore: auditStore}
}

// ListUsers handles GET /api/v1/admin/users
// Returns paginated list of all users with their roles (admin only).
// Emails are masked — use GET /admin/users/:id/reveal-email to see the raw value.
func (h *AdminHandler) ListUsers(c echo.Context) error {
	limit := parseIntParam(c.QueryParam("limit"), 50)
	offset := parseIntParam(c.QueryParam("offset"), 0)

	userList, total, err := h.store.ListUsers(c.Request().Context(), limit, offset)
	if err != nil {
		return common.Internal(c)
	}

	type userResponse struct {
		ID                 string   `json:"id"`
		Username           string   `json:"username"`
		Email              string   `json:"email"`
		DisplayName        string   `json:"display_name"`
		ForcePasswordReset bool     `json:"force_password_reset"`
		IsActive           bool     `json:"is_active"`
		Roles              []string `json:"roles"`
		Permissions        []string `json:"permissions"`
		LastLogin          *string  `json:"last_login,omitempty"`
	}

	out := make([]userResponse, 0, len(userList))
	for _, u := range userList {
		uCopy := u
		safe := Mask(&uCopy, false) // apply maskEmail transform; use reveal-email endpoint for raw value
		roles := u.Roles
		if roles == nil {
			roles = []string{}
		}
		perms := u.Permissions
		if perms == nil {
			perms = []string{}
		}
		var lastLogin *string
		if u.LastLoginAt != nil {
			s := u.LastLoginAt.Format("2006-01-02T15:04:05Z07:00")
			lastLogin = &s
		}
		out = append(out, userResponse{
			ID:                 u.ID,
			Username:           u.Username,
			Email:              safe.Email,
			DisplayName:        u.DisplayName,
			ForcePasswordReset: u.ForcePasswordReset,
			IsActive:           u.IsActive,
			Roles:              roles,
			Permissions:        perms,
			LastLogin:          lastLogin,
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"users":  out,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// RevealEmail handles GET /api/v1/admin/users/:id/reveal-email
// Returns the raw (unmasked) email for the given user and records an audit event.
// Requires sensitive_data:reveal permission.
func (h *AdminHandler) RevealEmail(c echo.Context) error {
	actorID, ok := c.Get("user_id").(string)
	if !ok || actorID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}

	targetID := c.Param("id")
	uw, err := h.store.GetWithRoles(c.Request().Context(), targetID)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "users.not_found", "User not found")
	}

	if h.auditStore != nil {
		h.auditStore.RecordReveal(c.Request().Context(),
			actorID, targetID, "email", "", c.RealIP())
	}

	return c.JSON(http.StatusOK, map[string]string{
		"id":    uw.ID,
		"email": uw.Email,
	})
}

// GetUser handles GET /api/v1/admin/users/:id
// Returns a single user with their roles (admin only). Emails are masked by
// default; callers with the sensitive_data:reveal permission must go through
// GET /admin/users/:id/reveal-email to see the raw value (which emits an audit
// event). This matches the masking applied by ListUsers.
func (h *AdminHandler) GetUser(c echo.Context) error {
	userID := c.Param("id")
	uw, err := h.store.GetWithRoles(c.Request().Context(), userID)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "users.not_found", "User not found")
	}

	roles := uw.Roles
	if roles == nil {
		roles = []string{}
	}
	perms := uw.Permissions
	if perms == nil {
		perms = []string{}
	}

	// Mask the email. Even admins must use the dedicated reveal-email endpoint
	// (which records an audit entry) to view the raw value.
	safe := Mask(uw, false)

	resp := map[string]any{
		"id":                   uw.ID,
		"username":             uw.Username,
		"email":                safe.Email,
		"display_name":         uw.DisplayName,
		"force_password_reset": uw.ForcePasswordReset,
		"is_active":            uw.IsActive,
		"roles":                roles,
		"permissions":          perms,
	}
	if uw.LastLoginAt != nil {
		resp["last_login"] = uw.LastLoginAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return c.JSON(http.StatusOK, resp)
}

// UpdateUserRoles handles PUT /api/v1/admin/users/:id/roles
// Replaces the user's role set (admin only). The change is recorded in the
// audit log with the previous and new role lists.
func (h *AdminHandler) UpdateUserRoles(c echo.Context) error {
	userID := c.Param("id")
	actorID, _ := c.Get("user_id").(string)

	var req struct {
		Roles []string `json:"roles"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	if req.Roles == nil {
		req.Roles = []string{}
	}

	// Capture previous roles for the audit trail before mutating.
	var prevRoles []string
	if u, err := h.store.GetWithRoles(c.Request().Context(), userID); err == nil && u != nil {
		prevRoles = u.Roles
	}

	if err := h.store.UpdateUserRoles(c.Request().Context(), userID, req.Roles); err != nil {
		return common.ErrorResponse(c, http.StatusUnprocessableEntity, "users.update_roles_failed", err.Error())
	}

	if h.auditStore != nil {
		// auditStore is the interface the existing reveal-email path uses; we
		// extend it here to capture role changes as a generic privileged write.
		if rec, ok := h.auditStore.(interface {
			RecordRoleChange(ctx context.Context, actorID, targetUserID string, oldRoles, newRoles []string, ipAddress string)
		}); ok {
			rec.RecordRoleChange(c.Request().Context(),
				actorID, userID, prevRoles, req.Roles, c.RealIP())
		}
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func parseIntParam(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return def
	}
	return v
}
