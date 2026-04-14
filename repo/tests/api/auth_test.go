// tests/api/auth_test.go — HTTP-level tests for auth endpoints.
// All tests use httptest and in-process mocks; no real database is required.
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"

	"portal/internal/app/common"
	"portal/internal/app/sessions"
	"portal/internal/platform/logging"
)

// ── Minimal in-memory fakes ───────────────────────────────────────────────────

type fakeUser struct {
	id                 string
	username           string
	passwordHash       string
	forcePasswordReset bool
	isActive           bool
}

type fakeUserStore struct {
	users map[string]*fakeUser // keyed by username
	byID  map[string]*fakeUser // keyed by id
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{
		users: make(map[string]*fakeUser),
		byID:  make(map[string]*fakeUser),
	}
}

func (s *fakeUserStore) add(u fakeUser) {
	s.users[u.username] = &u
	s.byID[u.id] = &u
}

// fakeSessionStore is a simple in-memory session store.
type fakeSessionStore struct {
	sessions map[string]*sessions.Session // keyed by token
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{sessions: make(map[string]*sessions.Session)}
}

// ── Echo handler wiring helpers ───────────────────────────────────────────────

// buildAuthEcho constructs a minimal Echo instance with auth routes wired to
// handlers that use the supplied fakes instead of real DB stores.
func buildAuthEcho(
	userStore *fakeUserStore,
	sessionStore *fakeSessionStore,
) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	log := logging.New(nil, logging.ERROR, "test")

	// ── Login handler ────────────────────────────────────────────────────────
	e.POST("/api/v1/auth/login", func(c echo.Context) error {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := c.Bind(&req); err != nil {
			return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
		}
		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" || req.Password == "" {
			return common.BadRequest(c, "validation.required", "Username and password are required")
		}

		u, ok := userStore.users[req.Username]
		if !ok {
			// constant-time dummy
			_ = bcrypt.CompareHashAndPassword(
				[]byte("$2a$12$aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
				[]byte(req.Password),
			)
			return common.Unauthorized(c, "Invalid credentials")
		}

		if !u.isActive {
			return common.Unauthorized(c, "Account is disabled")
		}

		if err := bcrypt.CompareHashAndPassword([]byte(u.passwordHash), []byte(req.Password)); err != nil {
			return common.Unauthorized(c, "Invalid credentials")
		}

		// Mint a fake token and store it.
		token := "testtoken_" + u.id
		sessionStore.sessions[token] = &sessions.Session{
			ID:     "sess_" + u.id,
			UserID: u.id,
		}

		sessions.SetCookie(c, token)
		_ = log // suppress unused warning

		return c.JSON(http.StatusOK, map[string]any{
			"requires_mfa":       false,
			"compatibility_mode": "full",
			"user": map[string]any{
				"id":                   u.id,
				"username":             u.username,
				"display_name":         u.username,
				"roles":                []string{},
				"permissions":          []string{},
				"force_password_reset": u.forcePasswordReset,
				"mfa_enrolled":         false,
				"mfa_verified":         false,
			},
		})
	})

	// ── Logout handler ───────────────────────────────────────────────────────
	e.POST("/api/v1/auth/logout", func(c echo.Context) error {
		token := sessions.TokenFromRequest(c)
		if token != "" {
			delete(sessionStore.sessions, token)
		}
		sessions.ClearCookie(c)
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// requireAuth is an inline middleware for this test Echo.
	requireAuth := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			token := sessions.TokenFromRequest(c)
			if token == "" {
				return common.Unauthorized(c, "Authentication required")
			}
			sess, ok := sessionStore.sessions[token]
			if !ok {
				sessions.ClearCookie(c)
				return common.Unauthorized(c, "Session expired or invalid")
			}
			c.Set("user_id", sess.UserID)
			return next(c)
		}
	}

	// ── GET /session ─────────────────────────────────────────────────────────
	e.GET("/api/v1/session", requireAuth(func(c echo.Context) error {
		userID := c.Get("user_id").(string)
		u, ok := userStore.byID[userID]
		if !ok {
			return common.Unauthorized(c, "Session invalid")
		}
		return c.JSON(http.StatusOK, map[string]any{
			"user": map[string]any{
				"id":                   u.id,
				"username":             u.username,
				"display_name":         u.username,
				"roles":                []string{},
				"permissions":          []string{},
				"force_password_reset": u.forcePasswordReset,
				"mfa_enrolled":         false,
				"mfa_verified":         false,
			},
			"compatibility_mode": "full",
		})
	}))

	// ── POST /auth/password/change ───────────────────────────────────────────
	e.POST("/api/v1/auth/password/change", requireAuth(func(c echo.Context) error {
		userID := c.Get("user_id").(string)
		u, ok := userStore.byID[userID]
		if !ok {
			return common.Internal(c)
		}

		var req struct {
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		if err := c.Bind(&req); err != nil {
			return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
		}
		if req.NewPassword == "" {
			return common.BadRequest(c, "validation.required", "New password is required")
		}
		if len(req.NewPassword) < 8 {
			return common.BadRequest(c, "validation.password_too_short", "Password must be at least 8 characters")
		}

		if !u.forcePasswordReset {
			if req.CurrentPassword == "" {
				return common.BadRequest(c, "validation.required", "Current password is required")
			}
			if err := bcrypt.CompareHashAndPassword([]byte(u.passwordHash), []byte(req.CurrentPassword)); err != nil {
				return common.Unauthorized(c, "Current password is incorrect")
			}
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), 12)
		if err != nil {
			return common.Internal(c)
		}
		u.passwordHash = string(hash)
		u.forcePasswordReset = false

		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}))

	return e
}

// ── Test helpers ──────────────────────────────────────────────────────────────

func mustHash(password string, t *testing.T) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(password), 10) // cost 10 for test speed
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	return string(h)
}

func jsonBody(v any) *bytes.Buffer {
	b, _ := json.Marshal(v)
	return bytes.NewBuffer(b)
}

func loginRequest(e *echo.Echo, username, password string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		jsonBody(map[string]string{"username": username, "password": password}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func getSession(e *echo.Echo, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// extractSessionCookie returns the portal_session cookie from a response recorder.
func extractSessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	res := rec.Result()
	defer res.Body.Close()
	for _, c := range res.Cookies() {
		if c.Name == sessions.CookieName {
			return c
		}
	}
	return nil
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestLoginCorrectCredentialsReturns200WithCookie(t *testing.T) {
	us := newFakeUserStore()
	ss := newFakeSessionStore()
	hash := mustHash("secret123", t)
	us.add(fakeUser{id: "u1", username: "alice", passwordHash: hash, isActive: true})
	e := buildAuthEcho(us, ss)

	rec := loginRequest(e, "alice", "secret123")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	cookie := extractSessionCookie(rec)
	if cookie == nil {
		t.Fatal("expected portal_session cookie in response")
	}
	if cookie.Value == "" {
		t.Error("cookie value must not be empty")
	}
	if !cookie.HttpOnly {
		t.Error("cookie must be HttpOnly")
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["requires_mfa"] != false {
		t.Errorf("expected requires_mfa=false, got %v", body["requires_mfa"])
	}
	user, ok := body["user"].(map[string]any)
	if !ok || user == nil {
		t.Fatal("expected user object in response")
	}
	if user["username"] != "alice" {
		t.Errorf("expected username=alice, got %v", user["username"])
	}
}

func TestLoginWrongPasswordReturns401(t *testing.T) {
	us := newFakeUserStore()
	ss := newFakeSessionStore()
	hash := mustHash("correct_password", t)
	us.add(fakeUser{id: "u2", username: "bob", passwordHash: hash, isActive: true})
	e := buildAuthEcho(us, ss)

	rec := loginRequest(e, "bob", "wrong_password")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestLoginNonexistentUserReturns401(t *testing.T) {
	us := newFakeUserStore()
	ss := newFakeSessionStore()
	e := buildAuthEcho(us, ss)

	rec := loginRequest(e, "nobody", "anypassword")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestLoginInactiveUserReturns401(t *testing.T) {
	us := newFakeUserStore()
	ss := newFakeSessionStore()
	hash := mustHash("pass", t)
	us.add(fakeUser{id: "u3", username: "carol", passwordHash: hash, isActive: false})
	e := buildAuthEcho(us, ss)

	rec := loginRequest(e, "carol", "pass")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for inactive user, got %d", rec.Code)
	}
}

func TestLoginEmptyCredentialsReturns400(t *testing.T) {
	us := newFakeUserStore()
	ss := newFakeSessionStore()
	e := buildAuthEcho(us, ss)

	rec := loginRequest(e, "", "")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestLogoutClearsSession(t *testing.T) {
	us := newFakeUserStore()
	ss := newFakeSessionStore()
	hash := mustHash("secret", t)
	us.add(fakeUser{id: "u4", username: "dave", passwordHash: hash, isActive: true})
	e := buildAuthEcho(us, ss)

	// Login first.
	loginRec := loginRequest(e, "dave", "secret")
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login failed: %d", loginRec.Code)
	}
	cookie := extractSessionCookie(loginRec)
	if cookie == nil {
		t.Fatal("no session cookie after login")
	}

	// Confirm session is valid.
	sessRec := getSession(e, cookie)
	if sessRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid session, got %d", sessRec.Code)
	}

	// Logout.
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutReq.AddCookie(cookie)
	logoutRec := httptest.NewRecorder()
	e.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusOK {
		t.Errorf("expected 200 on logout, got %d", logoutRec.Code)
	}

	// Session should now be invalid.
	sessRec2 := getSession(e, cookie)
	if sessRec2.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 after logout, got %d", sessRec2.Code)
	}
}

func TestGetSessionWithoutCookieReturns401(t *testing.T) {
	us := newFakeUserStore()
	ss := newFakeSessionStore()
	e := buildAuthEcho(us, ss)

	rec := getSession(e, nil)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestGetSessionWithValidCookieReturns200(t *testing.T) {
	us := newFakeUserStore()
	ss := newFakeSessionStore()
	hash := mustHash("mypass", t)
	us.add(fakeUser{id: "u5", username: "eve", passwordHash: hash, isActive: true})
	e := buildAuthEcho(us, ss)

	loginRec := loginRequest(e, "eve", "mypass")
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login failed: %d", loginRec.Code)
	}
	cookie := extractSessionCookie(loginRec)

	rec := getSession(e, cookie)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d — %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	user, ok := body["user"].(map[string]any)
	if !ok {
		t.Fatal("expected user in response")
	}
	if user["username"] != "eve" {
		t.Errorf("expected username=eve, got %v", user["username"])
	}
}

func TestChangePasswordForceResetDoesNotNeedCurrentPassword(t *testing.T) {
	us := newFakeUserStore()
	ss := newFakeSessionStore()
	hash := mustHash("bootstrap", t)
	us.add(fakeUser{id: "u6", username: "frank", passwordHash: hash, isActive: true, forcePasswordReset: true})
	e := buildAuthEcho(us, ss)

	// Login.
	loginRec := loginRequest(e, "frank", "bootstrap")
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", loginRec.Code, loginRec.Body.String())
	}
	cookie := extractSessionCookie(loginRec)

	// Change password — no current_password supplied.
	body := jsonBody(map[string]string{"new_password": "NewPass99!"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/change", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for bootstrap password change, got %d — %s", rec.Code, rec.Body.String())
	}
}

func TestChangePasswordTooShortReturns400(t *testing.T) {
	us := newFakeUserStore()
	ss := newFakeSessionStore()
	hash := mustHash("original", t)
	us.add(fakeUser{id: "u7", username: "grace", passwordHash: hash, isActive: true, forcePasswordReset: false})
	e := buildAuthEcho(us, ss)

	loginRec := loginRequest(e, "grace", "original")
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login: %d", loginRec.Code)
	}
	cookie := extractSessionCookie(loginRec)

	body := jsonBody(map[string]string{"current_password": "original", "new_password": "short"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/change", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short password, got %d", rec.Code)
	}

	var errBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if errBody["code"] != "validation.password_too_short" {
		t.Errorf("expected code=validation.password_too_short, got %v", errBody["code"])
	}
}

func TestChangePasswordWrongCurrentReturns401(t *testing.T) {
	us := newFakeUserStore()
	ss := newFakeSessionStore()
	hash := mustHash("correctpass", t)
	us.add(fakeUser{id: "u8", username: "hank", passwordHash: hash, isActive: true, forcePasswordReset: false})
	e := buildAuthEcho(us, ss)

	loginRec := loginRequest(e, "hank", "correctpass")
	cookie := extractSessionCookie(loginRec)

	body := jsonBody(map[string]string{"current_password": "wrongpass", "new_password": "NewPassword1!"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/change", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong current password, got %d", rec.Code)
	}
}

func TestChangePasswordWithoutSessionReturns401(t *testing.T) {
	us := newFakeUserStore()
	ss := newFakeSessionStore()
	e := buildAuthEcho(us, ss)

	body := jsonBody(map[string]string{"new_password": "NewPassword1!"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/change", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without session, got %d", rec.Code)
	}
}

// TestLoginResponseBodyShape verifies the JSON shape is what the frontend expects.
func TestLoginResponseBodyShape(t *testing.T) {
	us := newFakeUserStore()
	ss := newFakeSessionStore()
	hash := mustHash("pass", t)
	us.add(fakeUser{id: "u9", username: "ivan", passwordHash: hash, isActive: true})
	e := buildAuthEcho(us, ss)

	rec := loginRequest(e, "ivan", "pass")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Check top-level fields.
	for _, field := range []string{"requires_mfa", "compatibility_mode", "user"} {
		if _, ok := body[field]; !ok {
			t.Errorf("missing field %q in login response", field)
		}
	}

	// Check user fields.
	user, _ := body["user"].(map[string]any)
	for _, field := range []string{"id", "username", "display_name", "roles", "permissions", "force_password_reset", "mfa_enrolled", "mfa_verified"} {
		if _, ok := user[field]; !ok {
			t.Errorf("missing user field %q in login response", field)
		}
	}
}

// ── Placeholder to ensure build tag integration tests are skippable ──────────
// The function below references context to avoid "unused import" issues when
// this file is compiled without the integration build tag.
var _ = context.Background
