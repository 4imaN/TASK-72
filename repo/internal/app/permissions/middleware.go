// Package permissions provides Echo middleware for session validation and RBAC.
package permissions

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"portal/internal/app/common"
	appconfig "portal/internal/app/config"
	"portal/internal/app/mfa"
	"portal/internal/app/sessions"
	"portal/internal/app/users"
)

// Middleware holds the dependencies required for auth/RBAC middleware.
type Middleware struct {
	sessions *sessions.Store
	users    *users.Store
	mfa      *mfa.Store
	config   *appconfig.Store
}

// NewMiddleware constructs a Middleware with its stores.
func NewMiddleware(s *sessions.Store, u *users.Store, m *mfa.Store, cfg *appconfig.Store) *Middleware {
	return &Middleware{sessions: s, users: u, mfa: m, config: cfg}
}

// RequireAuth validates the session cookie on every request and injects
// user_id + session_id into the Echo context. It must be applied before any
// handler that needs an authenticated caller.
//
// MFA gate: if the user has MFA enrolled and the session is not yet MFA-verified,
// returns 403 with code "mfa_required" — EXCEPT for requests to /mfa/verify or
// /mfa/recovery paths which must remain accessible.
func (m *Middleware) RequireAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := sessions.TokenFromRequest(c)
		if token == "" {
			return common.Unauthorized(c, "Authentication required")
		}

		sess, err := m.sessions.Validate(c.Request().Context(), token)
		if err != nil || sess == nil {
			sessions.ClearCookie(c)
			return common.Unauthorized(c, "Session expired or invalid")
		}

		// Reject disabled accounts mid-session: an admin who flips is_active=FALSE
		// must immediately stop the user from making authenticated requests, even
		// if they still hold a valid cookie. We invalidate the session so the
		// cookie becomes unusable on subsequent requests too.
		if m.users != nil {
			if u, uErr := m.users.GetWithRoles(c.Request().Context(), sess.UserID); uErr == nil && u != nil && !u.IsActive {
				_ = m.sessions.Invalidate(c.Request().Context(), token)
				sessions.ClearCookie(c)
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"code":    "auth.account_disabled",
					"message": "Your account has been disabled",
				})
			}
		}

		c.Set("user_id", sess.UserID)
		c.Set("session_id", sess.ID)
		c.Set("mfa_verified", sess.MFAVerified)

		// MFA gate: skip for MFA verify/recovery endpoints themselves.
		path := c.Request().URL.Path
		isMFAPath := strings.Contains(path, "/mfa/verify") || strings.Contains(path, "/mfa/recovery")

		if !isMFAPath && m.mfa != nil {
			enrolled, _ := m.mfa.IsEnrolled(c.Request().Context(), sess.UserID)
			if enrolled && !sess.MFAVerified {
				return c.JSON(http.StatusForbidden, map[string]string{
					"code":    "mfa_required",
					"message": "MFA verification required",
				})
			}
		}

		// Client version compatibility gate.
		// Honors the compatibility.check_enabled feature flag — when an admin
		// disables it in the Config Center, all version-rule evaluation is
		// skipped so every client keeps full access. This gives operators a
		// kill-switch without having to delete rows from client_version_rules.
		if m.config != nil {
			clientVersion := c.Request().Header.Get("X-Client-Version")
			enforce, err := m.config.CheckFlag(c.Request().Context(), "compatibility.check_enabled", nil)
			if err != nil {
				// Fail-safe: if the flag cannot be read, enforce by default.
				enforce = true
			}
			if enforce && clientVersion != "" {
				rule, err := m.config.EvaluateClientVersion(c.Request().Context(), clientVersion)
				if err == nil && rule != nil {
					switch rule.Action {
					case "block":
						return c.JSON(http.StatusForbidden, map[string]string{
							"code":    "compatibility.blocked",
							"message": rule.Message,
						})
					case "read_only":
						method := c.Request().Method
						if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
							return c.JSON(http.StatusForbidden, map[string]string{
								"code":    "compatibility.read_only",
								"message": rule.Message,
							})
						}
					}
				}
			}
		}

		return next(c)
	}
}

// RequirePermission returns a middleware that enforces a single permission code.
// Must be chained after RequireAuth (relies on user_id in context).
func (m *Middleware) RequirePermission(permCode string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			userID, _ := c.Get("user_id").(string)
			if userID == "" {
				return common.Unauthorized(c, "Authentication required")
			}

			uw, err := m.users.GetWithRoles(c.Request().Context(), userID)
			if err != nil {
				return common.Unauthorized(c, "User not found")
			}

			for _, p := range uw.Permissions {
				if p == permCode {
					return next(c)
				}
			}
			return common.Forbidden(c, "Insufficient permissions")
		}
	}
}

// RequireRole returns a middleware that enforces a single role name.
// Must be chained after RequireAuth (relies on user_id in context).
func (m *Middleware) RequireRole(role string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			userID, _ := c.Get("user_id").(string)
			if userID == "" {
				return common.Unauthorized(c, "Authentication required")
			}

			uw, err := m.users.GetWithRoles(c.Request().Context(), userID)
			if err != nil {
				return common.Unauthorized(c, "User not found")
			}

			for _, r := range uw.Roles {
				if r == role {
					return next(c)
				}
			}
			return common.Forbidden(c, "Insufficient role")
		}
	}
}
