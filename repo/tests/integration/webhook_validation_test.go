// tests/integration/webhook_validation_test.go — ensures the LAN-only URL
// guard rejects public destinations through the real Echo + handler + DB
// stack, and that a successful create produces the row + secret rotation
// the handler promises.
package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"portal/internal/app/permissions"
	"portal/internal/app/mfa"
	"portal/internal/app/sessions"
	"portal/internal/app/users"
	appconfig "portal/internal/app/config"
	"portal/internal/app/webhooks"
	"portal/internal/platform/crypto"
)

// TestWebhookCreate_LANGate asserts:
//
//  1. Creating an endpoint pointed at a public IP is rejected with
//     code "webhooks.url_not_lan".
//  2. Creating an endpoint pointed at an RFC1918 IP succeeds and
//     returns a server-generated secret.
//  3. The persisted webhook_endpoints row exists with the correct URL.
func TestWebhookCreate_LANGate(t *testing.T) {
	h := Setup(t)
	defer h.Cleanup()
	ctx := context.Background()

	// Encryption key is required for webhook secret storage. Use a fixed
	// 32-byte key for reproducibility — production wires it from KMS/secrets.
	encryptor, err := crypto.NewEncryptorFromKey([]byte(strings.Repeat("a", 32)))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}

	userStore := users.NewStore(h.Pool)
	sessStore := sessions.NewStore(h.Pool)
	mfaStore  := mfa.NewStore(h.Pool, encryptor)
	cfgStore  := appconfig.NewStore(h.Pool)
	whStore   := webhooks.NewStore(h.Pool, encryptor)
	whHandler := webhooks.NewHandlerWithFlags(whStore, cfgStore, userStore)

	mw := permissions.NewMiddleware(sessStore, userStore, mfaStore, cfgStore)
	g := h.Echo.Group("/api/v1")
	g.Use(mw.RequireAuth)
	g.POST("/admin/webhooks", whHandler.CreateEndpoint, mw.RequireRole("admin"))

	adminID := h.MakeUser(ctx, "wendy-admin", "x", "admin")
	token   := h.SeedSession(ctx, adminID, true)

	// 1) Public IP rejected.
	rec := h.do(t, "POST", "/api/v1/admin/webhooks", token, map[string]any{
		"url":    "http://8.8.8.8/exfil",
		"events": []string{"export.completed"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("public IP must be rejected (400), got %d body=%s", rec.Code, rec.Body.String())
	}
	var errBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if code, _ := errBody["code"].(string); code != "webhooks.url_not_lan" {
		t.Errorf("expected code=webhooks.url_not_lan, got %q (body=%s)", code, rec.Body.String())
	}

	// 2) RFC1918 accepted; server-generated secret returned.
	rec = h.do(t, "POST", "/api/v1/admin/webhooks", token, map[string]any{
		"url":    "http://10.0.0.5/hook",
		"events": []string{"export.completed", "settlement.approved"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("RFC1918 URL must be accepted (201), got %d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created body: %v", err)
	}
	if secret, _ := created["secret"].(string); len(secret) < 32 {
		t.Errorf("expected server-generated secret of >=32 chars, got %q", secret)
	}
	endpointID, _ := created["id"].(string)

	// 3) Row exists in the DB.
	var url string
	if err := h.Pool.QueryRow(ctx,
		`SELECT url FROM webhook_endpoints WHERE id = $1`, endpointID,
	).Scan(&url); err != nil {
		t.Fatalf("read endpoint row: %v", err)
	}
	if url != "http://10.0.0.5/hook" {
		t.Errorf("persisted URL mismatch: got %q", url)
	}

	// 4) Localhost is also accepted.
	rec = h.do(t, "POST", "/api/v1/admin/webhooks", token, map[string]any{
		"url":    "http://localhost:9000/local-hook",
		"events": []string{"export.completed"},
	})
	if rec.Code != http.StatusCreated {
		t.Errorf("localhost URL must be accepted (201), got %d body=%s", rec.Code, rec.Body.String())
	}

	// 5) Non-http scheme is rejected.
	rec = h.do(t, "POST", "/api/v1/admin/webhooks", token, map[string]any{
		"url":    "ftp://10.0.0.5/exfil",
		"events": []string{"export.completed"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("non-http scheme must be rejected (400), got %d body=%s", rec.Code, rec.Body.String())
	}
}
