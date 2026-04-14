// tests/integration/mfa_session_test.go — real-stack integration tests for
// MFA/session gating using the actual permissions middleware chain, MFA store,
// and sessions store against a live PostgreSQL database.
//
// These tests replace the fake-middleware-only coverage in
// tests/security/auth_security_test.go with end-to-end verification that:
//   - An MFA-enrolled user without MFA verification receives 403 mfa_required.
//   - An MFA-enrolled user WITH MFA verification passes through.
//   - A user without MFA enrollment is not gated at all.
//   - The MFA gate does NOT apply to /mfa/verify and /mfa/recovery paths.
package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/labstack/echo/v4"

	appconfig "portal/internal/app/config"
	"portal/internal/app/mfa"
	"portal/internal/app/permissions"
	"portal/internal/app/sessions"
	"portal/internal/app/users"
)

// TestMFAGate_RealStack verifies the MFA gate in the RequireAuth middleware
// using real stores and a real database.
func TestMFAGate_RealStack(t *testing.T) {
	h := Setup(t)
	defer h.Cleanup()
	ctx := context.Background()

	userStore := users.NewStore(h.Pool)
	sessStore := sessions.NewStore(h.Pool)
	mfaStore := mfa.NewStore(h.Pool, nil)
	cfgStore := appconfig.NewStore(h.Pool)

	mw := permissions.NewMiddleware(sessStore, userStore, mfaStore, cfgStore)
	g := h.Echo.Group("/api/v1")
	g.Use(mw.RequireAuth)

	// A protected endpoint that should be gated by MFA when applicable.
	g.GET("/protected", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// MFA verification endpoint — should be exempt from the MFA gate.
	g.POST("/mfa/verify", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"mfa": "verified"})
	})

	// ── Setup users ──────────────────────────────────────────────────────────
	enrolledUser := h.MakeUser(ctx, "mfa-enrolled-user", "password1", "learner")
	unenrolledUser := h.MakeUser(ctx, "no-mfa-user", "password2", "learner")

	// Seed a confirmed MFA enrollment for enrolledUser.
	_, err := h.Pool.Exec(ctx, `
		INSERT INTO mfa_totp_enrollments (user_id, encrypted_secret, confirmed)
		VALUES ($1, 'fake-encrypted-secret', TRUE)`, enrolledUser)
	if err != nil {
		t.Fatalf("seed MFA enrollment: %v", err)
	}

	// ── Test 1: MFA-enrolled, session NOT MFA-verified → 403 ────────────────
	t.Run("enrolled_not_verified_gets_403", func(t *testing.T) {
		token := h.SeedSession(ctx, enrolledUser, false) // mfa_verified=false
		rec := h.do(t, "GET", "/api/v1/protected", token, nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d — body: %s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["code"] != "mfa_required" {
			t.Errorf("expected code=mfa_required, got %v", body["code"])
		}
	})

	// ── Test 2: MFA-enrolled, session MFA-verified → 200 ────────────────────
	t.Run("enrolled_and_verified_gets_200", func(t *testing.T) {
		token := h.SeedSession(ctx, enrolledUser, true) // mfa_verified=true
		rec := h.do(t, "GET", "/api/v1/protected", token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
		}
	})

	// ── Test 3: Not MFA-enrolled → 200 (no gate applied) ────────────────────
	t.Run("unenrolled_user_passes_through", func(t *testing.T) {
		token := h.SeedSession(ctx, unenrolledUser, false) // no MFA at all
		rec := h.do(t, "GET", "/api/v1/protected", token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
		}
	})

	// ── Test 4: MFA-enrolled but hitting /mfa/verify → exempt (200) ─────────
	t.Run("mfa_verify_endpoint_exempt_from_gate", func(t *testing.T) {
		token := h.SeedSession(ctx, enrolledUser, false) // not yet verified
		rec := h.do(t, "POST", "/api/v1/mfa/verify", token, map[string]any{"code": "123456"})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 (MFA verify exempt), got %d — body: %s", rec.Code, rec.Body.String())
		}
	})

	// ── Test 5: No session at all → 401 ─────────────────────────────────────
	t.Run("no_session_gets_401", func(t *testing.T) {
		rec := h.do(t, "GET", "/api/v1/protected", "", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	// ── Test 6: Disabled account mid-session → 401 ──────────────────────────
	t.Run("disabled_account_gets_401", func(t *testing.T) {
		disabledUser := h.MakeUser(ctx, "disabled-user", "password3", "learner")
		token := h.SeedSession(ctx, disabledUser, false)

		// Deactivate the user after session creation.
		_, err := h.Pool.Exec(ctx, `UPDATE users SET is_active = FALSE WHERE id = $1`, disabledUser)
		if err != nil {
			t.Fatalf("deactivate user: %v", err)
		}

		rec := h.do(t, "GET", "/api/v1/protected", token, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for disabled account, got %d — body: %s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["code"] != "auth.account_disabled" {
			t.Errorf("expected code=auth.account_disabled, got %v", body["code"])
		}
	})
}
