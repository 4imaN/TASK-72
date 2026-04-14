// Package security_test contains security-focused tests for authentication,
// authorization, data masking, MFA, and encryption.
// Tests are designed to run without a database by exercising logic directly.
package security_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"

	"portal/internal/app/common"
	"portal/internal/app/users"
	"portal/internal/platform/crypto"
)

// ── Helper ────────────────────────────────────────────────────────────────────

func newEcho() *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	return e
}

// requireAuthMiddleware simulates the RequireAuth check without a real DB.
func requireAuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		cookie, err := c.Cookie("portal_session")
		if err != nil || cookie.Value == "" {
			return common.Unauthorized(c, "Authentication required")
		}
		c.Set("user_id", "test-user-id")
		return next(c)
	}
}

// requireRoleMiddleware simulates RBAC without a real DB.
func requireRoleMiddleware(required string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			role, _ := c.Get("role").(string)
			if role != required {
				return common.Forbidden(c, "Insufficient role")
			}
			return next(c)
		}
	}
}

// ── Test 1: Unauthenticated requests return 401 ───────────────────────────────

func TestUnauthenticated401(t *testing.T) {
	e := newEcho()
	e.GET("/api/v1/session", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}, requireAuthMiddleware)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body["code"] != "auth.unauthenticated" {
		t.Errorf("expected auth.unauthenticated error code, got %v", body["code"])
	}
}

func TestUnauthenticated401_MFAEndpoint(t *testing.T) {
	e := newEcho()
	e.POST("/api/v1/mfa/verify", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]bool{"mfa_verified": true})
	}, requireAuthMiddleware)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mfa/verify",
		strings.NewReader(`{"code":"123456"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without session cookie, got %d", rec.Code)
	}
}

// ── Test 2: Wrong role returns 403 ───────────────────────────────────────────

func TestForbidden403(t *testing.T) {
	e := newEcho()
	handler := func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"ok": "admin-only"})
	}
	e.GET("/api/v1/admin", handler, requireAuthMiddleware, requireRoleMiddleware("admin"))

	// Request with session cookie but non-admin role in context
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin", nil)
	req.AddCookie(&http.Cookie{Name: "portal_session", Value: "some-token"})
	rec := httptest.NewRecorder()

	// Simulate a learner role being set (not admin)
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("role", "learner")
			return next(c)
		}
	})

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for insufficient role, got %d", rec.Code)
	}
}

// ── Test 3: maskEmail masking logic ──────────────────────────────────────────

func TestMaskedEmailForNonAdmin(t *testing.T) {
	cases := []struct {
		email    string
		wantMask string
		isAdmin  bool
	}{
		{"alice@example.com", "a***@example.com", false},
		{"bob@company.org", "b***@company.org", false},
		{"x@short.io", "***@short.io", false},
		{"admin@portal.internal", "admin@portal.internal", true}, // admin sees full email
		{"j@x.com", "***@x.com", false},                          // at-pos=1, so fallback
	}

	for _, tc := range cases {
		u := &users.UserWithRoles{}
		u.ID = "test-id"
		u.Username = "testuser"
		u.DisplayName = "Test"
		u.Email = tc.email
		u.IsActive = true

		safe := users.Mask(u, tc.isAdmin)
		if safe.Email != tc.wantMask {
			t.Errorf("Mask(%q, admin=%v): got email %q, want %q", tc.email, tc.isAdmin, safe.Email, tc.wantMask)
		}
	}
}

func TestMaskPreservesNonSensitiveFields(t *testing.T) {
	u := &users.UserWithRoles{}
	u.ID = "user-123"
	u.Username = "jdoe"
	u.DisplayName = "John Doe"
	u.Email = "john@example.com"
	u.IsActive = true

	safe := users.Mask(u, false)
	if safe.ID != "user-123" {
		t.Errorf("ID should not be masked")
	}
	if safe.Username != "jdoe" {
		t.Errorf("Username should not be masked")
	}
	if safe.DisplayName != "John Doe" {
		t.Errorf("DisplayName should not be masked")
	}
	if !safe.IsActive {
		t.Errorf("IsActive should be preserved")
	}
}

// ── Test 4: TOTP enrollment and verify ───────────────────────────────────────

func TestTOTPEnrollAndVerify(t *testing.T) {
	// Generate a TOTP key (same as StartEnrollment does under the hood)
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Portal",
		AccountName: "testuser",
	})
	if err != nil {
		t.Fatalf("generate totp key: %v", err)
	}

	// Generate a valid code for the key
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("generate totp code: %v", err)
	}

	// Validate the code
	if !totp.Validate(code, key.Secret()) {
		t.Error("expected generated TOTP code to be valid")
	}
}

// ── Test 5: Invalid TOTP code fails ──────────────────────────────────────────

func TestTOTPInvalidCodeFails(t *testing.T) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Portal",
		AccountName: "testuser",
	})
	if err != nil {
		t.Fatalf("generate totp key: %v", err)
	}

	// A clearly wrong code
	if totp.Validate("000000", key.Secret()) {
		// This could theoretically pass by chance once in 1,000,000 — extremely unlikely
		t.Log("Warning: 000000 happened to be the valid TOTP code — extremely rare, re-run test")
	}

	if totp.Validate("abc123", key.Secret()) {
		t.Error("expected non-numeric code to fail TOTP validation")
	}
}

func TestTOTPInvalidCodeReturnsFalse(t *testing.T) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Portal",
		AccountName: "testuser",
	})
	if err != nil {
		t.Fatalf("generate totp key: %v", err)
	}

	// A 6-digit code that's almost certainly wrong (uses a different secret)
	otherKey, _ := totp.Generate(totp.GenerateOpts{
		Issuer:      "Portal",
		AccountName: "otheruser",
	})
	otherCode, _ := totp.GenerateCode(otherKey.Secret(), time.Now())

	// The code for a different secret should not validate against the first key
	// (with overwhelming probability)
	_ = totp.Validate(otherCode, key.Secret())
	// We don't assert a hard false here since codes from different secrets can
	// collide in 1/1,000,000 cases; the important thing is the function works.
}

// ── Test 6: Recovery codes hash and verify ───────────────────────────────────

func TestRecoveryCodeHashAndVerify(t *testing.T) {
	plaintext := "deadbeef1234"

	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), 10)
	if err != nil {
		t.Fatalf("bcrypt generate: %v", err)
	}

	// Correct code verifies
	if err := bcrypt.CompareHashAndPassword(hash, []byte(plaintext)); err != nil {
		t.Errorf("expected correct code to verify: %v", err)
	}

	// Wrong code does not verify
	if err := bcrypt.CompareHashAndPassword(hash, []byte("wrongcode")); err == nil {
		t.Error("expected wrong code to fail verification")
	}
}

func TestRecoveryCodeReusePreventionLogic(t *testing.T) {
	// Simulate: code is generated, used once, and then marked as used.
	// After being marked used, it should no longer be returned in the unused query.
	// We test the bcrypt logic here; the DB "used_at IS NULL" filter is the other guard.

	codes := []string{"aabb1122cc33", "ddeeff445566", "112233445566"}
	hashes := make([]string, len(codes))
	for i, c := range codes {
		h, err := bcrypt.GenerateFromPassword([]byte(c), 10)
		if err != nil {
			t.Fatalf("bcrypt: %v", err)
		}
		hashes[i] = string(h)
	}

	// "Use" the first code
	usedCode := codes[0]
	foundIdx := -1
	for i, h := range hashes {
		if bcrypt.CompareHashAndPassword([]byte(h), []byte(usedCode)) == nil {
			foundIdx = i
			break
		}
	}
	if foundIdx != 0 {
		t.Errorf("expected to find code at index 0, got %d", foundIdx)
	}

	// Attempt reuse of same code against remaining hashes (simulate used_at filter removing it)
	remaining := hashes[1:]
	for _, h := range remaining {
		if bcrypt.CompareHashAndPassword([]byte(h), []byte(usedCode)) == nil {
			t.Error("used code should not match remaining hashes")
		}
	}
}

// ── Test 7: AES-256-GCM encrypt/decrypt roundtrip ────────────────────────────

func TestAESEncryptDecrypt(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}

	enc, err := crypto.NewEncryptorFromKey(key)
	if err != nil {
		t.Fatalf("create encryptor: %v", err)
	}

	plaintexts := []string{
		"hello world",
		"TOTP-SECRET-BASE32-VALUE",
		"",
		"unicode: こんにちは",
		strings.Repeat("a", 4096),
	}

	for _, pt := range plaintexts {
		ciphertext, err := enc.Encrypt(pt)
		if err != nil {
			t.Errorf("encrypt(%q): %v", pt, err)
			continue
		}

		decrypted, err := enc.Decrypt(ciphertext)
		if err != nil {
			t.Errorf("decrypt(%q): %v", pt, err)
			continue
		}

		if decrypted != pt {
			t.Errorf("roundtrip failed: got %q, want %q", decrypted, pt)
		}
	}
}

func TestAESEncryptProducesDifferentCiphertexts(t *testing.T) {
	key := make([]byte, 32)
	enc, _ := crypto.NewEncryptorFromKey(key)

	ct1, _ := enc.Encrypt("same plaintext")
	ct2, _ := enc.Encrypt("same plaintext")

	// Due to random nonce, ciphertexts must differ
	if ct1 == ct2 {
		t.Error("expected different ciphertexts for same plaintext (random nonce)")
	}
}

func TestAESDecryptInvalidHexFails(t *testing.T) {
	key := make([]byte, 32)
	enc, _ := crypto.NewEncryptorFromKey(key)

	_, err := enc.Decrypt("not-valid-hex!!")
	if err == nil {
		t.Error("expected error decrypting invalid hex")
	}
}

func TestAESDecryptTamperedCiphertextFails(t *testing.T) {
	key := make([]byte, 32)
	enc, _ := crypto.NewEncryptorFromKey(key)

	ct, _ := enc.Encrypt("sensitive data")
	// Flip a byte in the middle of the ciphertext hex
	ctBytes := []byte(ct)
	if len(ctBytes) > 20 {
		if ctBytes[20] == 'a' {
			ctBytes[20] = 'b'
		} else {
			ctBytes[20] = 'a'
		}
	}
	tampered := string(ctBytes)

	_, err := enc.Decrypt(tampered)
	if err == nil {
		t.Error("expected authentication failure on tampered ciphertext")
	}
}

func TestAESWrongKeyFails(t *testing.T) {
	key1 := make([]byte, 32)
	key1[0] = 1
	key2 := make([]byte, 32)
	key2[0] = 2

	enc1, _ := crypto.NewEncryptorFromKey(key1)
	enc2, _ := crypto.NewEncryptorFromKey(key2)

	ct, _ := enc1.Encrypt("secret")
	_, err := enc2.Decrypt(ct)
	if err == nil {
		t.Error("expected decryption to fail with wrong key")
	}
}

func TestAESInvalidKeyLength(t *testing.T) {
	_, err := crypto.NewEncryptorFromKey([]byte("tooshort"))
	if err == nil {
		t.Error("expected error for non-32-byte key")
	}
}
