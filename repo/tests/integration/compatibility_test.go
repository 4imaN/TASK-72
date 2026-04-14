// tests/integration/compatibility_test.go — real-stack integration tests for
// client version compatibility enforcement (read_only and blocked modes).
//
// These tests verify that:
//   - A client with a blocked version gets 403 on all requests.
//   - A client in read_only mode can GET but POST/PUT/DELETE are blocked.
//   - A current-version client is not affected.
//   - When the compatibility.check_enabled flag is disabled, no version rules apply.
package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	appconfig "portal/internal/app/config"
	"portal/internal/app/mfa"
	"portal/internal/app/permissions"
	"portal/internal/app/sessions"
	"portal/internal/app/users"
)

// TestCompatibilityMode_RealStack verifies client version enforcement using
// the real middleware chain, config store, and version rules table.
func TestCompatibilityMode_RealStack(t *testing.T) {
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

	g.GET("/compat-test", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	g.POST("/compat-test", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "created"})
	})

	testUser := h.MakeUser(ctx, "compat-user", "pw", "learner")
	token := h.SeedSession(ctx, testUser, false)

	// Seed version rules:
	//   - min_version=2.0.0 action=block → clients below 2.0.0 are blocked
	//   - min_version=1.5.0 max_version=2.0.0 action=read_only → clients in [1.5.0, 2.0.0) are read_only
	err := cfgStore.SetVersionRule(ctx, "2.0.0", "", "block", "Unsupported version", time.Time{})
	if err != nil {
		t.Fatalf("set block rule: %v", err)
	}
	err = cfgStore.SetVersionRule(ctx, "1.5.0", "2.0.0", "read_only", "Please upgrade", time.Time{})
	if err != nil {
		t.Fatalf("set read_only rule: %v", err)
	}

	// Helper that sets X-Client-Version header.
	doWithVersion := func(t *testing.T, method, path, tok, version string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Client-Version", version)
		if tok != "" {
			req.AddCookie(&http.Cookie{Name: sessions.CookieName, Value: tok})
		}
		h.Echo.ServeHTTP(rec, req)
		return rec
	}

	// ── Test 1: Blocked client gets 403 on GET ──────────────────────────────
	t.Run("blocked_version_GET_returns_403", func(t *testing.T) {
		rec := doWithVersion(t, "GET", "/api/v1/compat-test", token, "1.0.0")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for blocked client, got %d — body: %s", rec.Code, rec.Body.String())
		}
	})

	// ── Test 2: Blocked client gets 403 on POST ─────────────────────────────
	t.Run("blocked_version_POST_returns_403", func(t *testing.T) {
		rec := doWithVersion(t, "POST", "/api/v1/compat-test", token, "1.0.0")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for blocked client POST, got %d", rec.Code)
		}
	})

	// ── Test 3: Read-only client can GET ────────────────────────────────────
	t.Run("read_only_version_GET_returns_200", func(t *testing.T) {
		rec := doWithVersion(t, "GET", "/api/v1/compat-test", token, "1.8.0")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for read_only client GET, got %d — body: %s", rec.Code, rec.Body.String())
		}
	})

	// ── Test 4: Read-only client cannot POST ────────────────────────────────
	t.Run("read_only_version_POST_returns_403", func(t *testing.T) {
		rec := doWithVersion(t, "POST", "/api/v1/compat-test", token, "1.8.0")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for read_only client POST, got %d — body: %s", rec.Code, rec.Body.String())
		}
	})

	// ── Test 5: Current version passes through ──────────────────────────────
	t.Run("current_version_passes", func(t *testing.T) {
		rec := doWithVersion(t, "POST", "/api/v1/compat-test", token, "3.0.0")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for current client, got %d — body: %s", rec.Code, rec.Body.String())
		}
	})

	// ── Test 6: Disabled flag bypasses all version checks ───────────────────
	t.Run("disabled_flag_bypasses_version_check", func(t *testing.T) {
		// Disable the compatibility check flag.
		err := cfgStore.SetFlag(ctx, "compatibility.check_enabled", false, 0, nil)
		if err != nil {
			t.Fatalf("disable flag: %v", err)
		}

		// A blocked version should now pass.
		rec := doWithVersion(t, "POST", "/api/v1/compat-test", token, "1.0.0")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 when check disabled, got %d — body: %s", rec.Code, rec.Body.String())
		}

		// Re-enable the flag for subsequent tests.
		_ = cfgStore.SetFlag(ctx, "compatibility.check_enabled", true, 100, nil)
	})
}
