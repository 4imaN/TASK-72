// tests/integration/login_mfa_e2e_test.go — end-to-end integration test for
// the full login → MFA verify → protected route flow using real handlers,
// real sessions, and real TOTP verification.
//
// This test proves the complete auth chain works end-to-end:
//   1. POST /auth/login with correct credentials → session cookie + requires_mfa=true
//   2. POST /mfa/verify with valid TOTP code → session upgraded to mfa_verified
//   3. GET /protected with the same cookie → 200 (gate passes)
//   4. Before MFA verify, GET /protected → 403 mfa_required
package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/pquerna/otp/totp"

	"portal/internal/app/auth"
	appconfig "portal/internal/app/config"
	"portal/internal/app/mfa"
	"portal/internal/app/permissions"
	"portal/internal/app/sessions"
	"portal/internal/app/users"
	"portal/internal/platform/crypto"
	"portal/internal/platform/logging"
)

// TestLoginMFAVerify_EndToEnd exercises the full auth flow with real handlers.
func TestLoginMFAVerify_EndToEnd(t *testing.T) {
	h := Setup(t)
	defer h.Cleanup()
	ctx := context.Background()

	userStore := users.NewStore(h.Pool)
	sessStore := sessions.NewStore(h.Pool)
	cfgStore := appconfig.NewStore(h.Pool)
	log := logging.New(io.Discard, logging.ERROR, "test")

	// Create an encryptor for MFA secrets.
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 42)
	}
	enc, err := crypto.NewEncryptorFromKey(key)
	if err != nil {
		t.Fatalf("create encryptor: %v", err)
	}
	mfaStore := mfa.NewStore(h.Pool, enc)

	authHandler := auth.NewHandler(userStore, sessStore, mfaStore, cfgStore, log)
	mfaHandler := mfa.NewHandler(mfaStore, sessStore, userStore, log)
	permMW := permissions.NewMiddleware(sessStore, userStore, mfaStore, cfgStore)

	// ── Route registration (mirrors cmd/api/main.go) ─────────────────────────
	v1 := h.Echo.Group("/api/v1")
	v1.POST("/auth/login", authHandler.Login)

	protected := v1.Group("", permMW.RequireAuth)
	protected.POST("/mfa/verify", mfaHandler.Verify)
	protected.GET("/protected", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// ── Create user with a real password ─────────────────────────────────────
	password := "S3cure!Pass#2026"
	testUser := h.MakeUser(ctx, "e2e-mfa-user", password, "learner")

	// ── Enroll MFA via the real store ────────────────────────────────────────
	enrollment, err := mfaStore.StartEnrollment(ctx, testUser, "e2e-mfa-user")
	if err != nil {
		t.Fatalf("start enrollment: %v", err)
	}
	// Confirm enrollment with a valid TOTP code.
	confirmCode, err := totp.GenerateCode(enrollment.SecretPlaintext, time.Now())
	if err != nil {
		t.Fatalf("generate confirm code: %v", err)
	}
	if err := mfaStore.ConfirmEnrollment(ctx, testUser, confirmCode); err != nil {
		t.Fatalf("confirm enrollment: %v", err)
	}

	// Helper to extract session cookie from response.
	extractCookie := func(rec *httptest.ResponseRecorder) *http.Cookie {
		t.Helper()
		for _, c := range rec.Result().Cookies() {
			if c.Name == sessions.CookieName {
				return c
			}
		}
		return nil
	}

	// Helper to issue requests.
	doReq := func(method, path string, cookie *http.Cookie, body any) *httptest.ResponseRecorder {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, path, &buf)
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		h.Echo.ServeHTTP(rec, req)
		return rec
	}

	// ── Step 1: Login ────────────────────────────────────────────────────────
	loginRec := doReq("POST", "/api/v1/auth/login", nil, map[string]string{
		"username": "e2e-mfa-user",
		"password": password,
	})
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d — body: %s", loginRec.Code, loginRec.Body.String())
	}

	var loginResp map[string]any
	_ = json.Unmarshal(loginRec.Body.Bytes(), &loginResp)
	if loginResp["requires_mfa"] != true {
		t.Fatalf("expected requires_mfa=true, got %v", loginResp["requires_mfa"])
	}

	sessionCookie := extractCookie(loginRec)
	if sessionCookie == nil {
		t.Fatal("no session cookie set on login response")
	}

	// ── Step 2: Before MFA verify, protected route returns 403 ──────────────
	t.Run("protected_before_mfa_returns_403", func(t *testing.T) {
		rec := doReq("GET", "/api/v1/protected", sessionCookie, nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 before MFA verify, got %d — body: %s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["code"] != "mfa_required" {
			t.Errorf("expected code=mfa_required, got %v", body["code"])
		}
	})

	// ── Step 3: MFA verify with real TOTP code ──────────────────────────────
	totpCode, err := totp.GenerateCode(enrollment.SecretPlaintext, time.Now())
	if err != nil {
		t.Fatalf("generate totp code: %v", err)
	}

	mfaRec := doReq("POST", "/api/v1/mfa/verify", sessionCookie, map[string]string{
		"code": totpCode,
	})
	if mfaRec.Code != http.StatusOK {
		t.Fatalf("mfa verify: expected 200, got %d — body: %s", mfaRec.Code, mfaRec.Body.String())
	}

	var mfaResp map[string]any
	_ = json.Unmarshal(mfaRec.Body.Bytes(), &mfaResp)
	if mfaResp["mfa_verified"] != true {
		t.Errorf("expected mfa_verified=true, got %v", mfaResp["mfa_verified"])
	}

	// ── Step 4: After MFA verify, protected route returns 200 ───────────────
	t.Run("protected_after_mfa_returns_200", func(t *testing.T) {
		rec := doReq("GET", "/api/v1/protected", sessionCookie, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 after MFA verify, got %d — body: %s", rec.Code, rec.Body.String())
		}
	})

	// ── Step 5: Wrong TOTP code is rejected ─────────────────────────────────
	t.Run("wrong_totp_code_rejected", func(t *testing.T) {
		// Create a fresh session (not MFA verified) for this sub-test.
		freshToken := h.SeedSession(ctx, testUser, false)
		freshCookie := &http.Cookie{Name: sessions.CookieName, Value: freshToken}
		rec := doReq("POST", "/api/v1/mfa/verify", freshCookie, map[string]string{
			"code": "000000",
		})
		// Should fail validation (likely 400 or 401).
		if rec.Code == http.StatusOK {
			t.Error("expected wrong TOTP code to be rejected, but got 200")
		}
	})
}
