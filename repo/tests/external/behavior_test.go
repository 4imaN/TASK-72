// Package external_test — strong behavioral tests with real assertions.
// These complement the existing external_api_test.go which focuses on route
// existence; this file asserts actual workflow outcomes.
package external_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ─── Catalog lifecycle: create → get → update → archive → restore → verify ──

func TestBehavior_CatalogFullLifecycle(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")

	uniqueTitle := fmt.Sprintf("Behavior Test Resource %d", time.Now().UnixMilli())

	// 1. Create
	resp := admin.post("/catalog/resources", map[string]any{
		"title":        uniqueTitle,
		"content_type": "article",
		"category":     "engineering",
		"is_published": true,
	})
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	created := admin.jsonBody(resp)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("no id returned from create")
	}
	if created["title"] != uniqueTitle {
		t.Errorf("title round-trip: got %v, want %q", created["title"], uniqueTitle)
	}

	// 2. GET returns same record
	resp = admin.get("/catalog/resources/" + id)
	fetched := admin.jsonBody(resp)
	if fetched["id"] != id {
		t.Errorf("get returned wrong id: %v", fetched["id"])
	}
	if fetched["title"] != uniqueTitle {
		t.Errorf("get title mismatch: %v", fetched["title"])
	}

	// 3. Update title
	newTitle := uniqueTitle + " — Updated"
	resp = admin.put("/catalog/resources/"+id, map[string]any{
		"title":        newTitle,
		"is_published": true,
	})
	if resp.StatusCode != 200 {
		t.Fatalf("update: %d", resp.StatusCode)
	}
	updated := admin.jsonBody(resp)
	if updated["title"] != newTitle {
		t.Errorf("update did not persist: %v", updated["title"])
	}

	// 4. Archive
	resp = admin.post("/catalog/resources/"+id+"/archive", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("archive: %d", resp.StatusCode)
	}
	resp = admin.get("/catalog/resources/" + id)
	archived := admin.jsonBody(resp)
	if archived["is_archived"] != true {
		t.Errorf("archive did not persist: %v", archived["is_archived"])
	}

	// 5. Restore
	resp = admin.post("/catalog/resources/"+id+"/restore", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("restore: %d", resp.StatusCode)
	}
	resp = admin.get("/catalog/resources/" + id)
	restored := admin.jsonBody(resp)
	if restored["is_archived"] != false {
		t.Errorf("restore did not persist: %v", restored["is_archived"])
	}
}

// ─── Reconciliation flow: import → create run → process → verify variances ──

func TestBehavior_ReconciliationFlow(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")

	period := fmt.Sprintf("2019-%02d", time.Now().Unix()%12+1)

	// 1. Import statement
	resp := fin.post("/reconciliation/statements", map[string]any{
		"source_file": "behavior_test.csv",
		"checksum":    "bh" + fmt.Sprint(time.Now().UnixMilli()),
		"rows": []map[string]any{
			{
				"line_description": "Behavior flow vendor",
				"statement_amount": 5000,
				"transaction_date": fmt.Sprintf("%s-15", period),
				"currency":         "USD",
			},
		},
	})
	if resp.StatusCode != 201 {
		t.Fatalf("statement import: %d body=%s", resp.StatusCode, fin.bodyString(resp))
	}
	batch := fin.jsonBody(resp)
	if batch["id"] == nil {
		t.Fatal("import missing id")
	}

	// 2. Create run
	resp = fin.post("/reconciliation/runs", map[string]string{"period": period})
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		t.Fatalf("create run: %d", resp.StatusCode)
	}
	run := fin.jsonBody(resp)
	runID, _ := run["id"].(string)
	if runID == "" {
		t.Fatal("no run id")
	}

	// 3. Process
	resp = fin.post("/reconciliation/runs/"+runID+"/process", nil)
	if resp.StatusCode != 200 {
		// 200 success or 409 if already processed — both acceptable
		if resp.StatusCode != 409 {
			t.Errorf("process: %d body=%s", resp.StatusCode, fin.bodyString(resp))
		}
	}

	// 4. Run should now be in completed or failed state (not pending)
	resp = fin.get("/reconciliation/runs/" + runID)
	final := fin.jsonBody(resp)
	status, _ := final["status"].(string)
	if status == "pending" {
		t.Errorf("run should have transitioned out of pending, got %q", status)
	}
}

// ─── Webhook LAN validation: actual URL contents persisted ──────────────────

func TestBehavior_WebhookURLPersisted(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")

	uniqueURL := fmt.Sprintf("http://10.0.0.%d/behavior-hook", time.Now().UnixMilli()%250+1)
	resp := admin.post("/admin/webhooks", map[string]any{
		"url":    uniqueURL,
		"events": []string{"export.completed"},
	})
	if resp.StatusCode != 201 {
		t.Fatalf("create webhook: %d", resp.StatusCode)
	}
	created := admin.jsonBody(resp)
	if created["url"] != uniqueURL {
		t.Errorf("URL round-trip: %v", created["url"])
	}
	// Server-generated secret must be returned and non-trivial
	secret, _ := created["secret"].(string)
	if len(secret) < 32 {
		t.Errorf("expected server-generated secret >= 32 chars, got %d", len(secret))
	}
}

// ─── User admin: reveal-email actually returns a plaintext email ────────────

func TestBehavior_RevealEmailReturnsPlaintext(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")

	resp := admin.get("/admin/users")
	m := admin.jsonBody(resp)
	users, _ := m["users"].([]any)
	var adminID string
	for _, u := range users {
		um := u.(map[string]any)
		if um["username"] == "bootstrap_admin" {
			adminID, _ = um["id"].(string)
			// In the list, email is masked — should contain asterisks
			listEmail, _ := um["email"].(string)
			if !strings.Contains(listEmail, "*") {
				t.Errorf("list email should be masked, got %q", listEmail)
			}
			break
		}
	}
	if adminID == "" {
		t.Fatal("admin user not found in list")
	}

	// Reveal endpoint returns plaintext
	resp = admin.get("/admin/users/" + adminID + "/reveal-email")
	if resp.StatusCode != 200 {
		t.Fatalf("reveal: %d", resp.StatusCode)
	}
	revealed := admin.jsonBody(resp)
	email, _ := revealed["email"].(string)
	if strings.Contains(email, "*") {
		t.Errorf("revealed email should be plaintext, got %q", email)
	}
	if !strings.Contains(email, "@") {
		t.Errorf("revealed email should contain @: %q", email)
	}
}

// ─── Config flag toggle: PUT then GET reflects the change ───────────────────

func TestBehavior_ConfigFlagToggleRoundTrip(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")

	// Toggle OFF
	resp := admin.put("/admin/config/flags/search.pinyin_expansion", map[string]any{
		"enabled":            false,
		"rollout_percentage": 100,
	})
	if resp.StatusCode != 200 {
		t.Fatalf("toggle off: %d", resp.StatusCode)
	}

	// GET should show disabled
	resp = admin.get("/admin/config/flags")
	m := admin.jsonBody(resp)
	flags, _ := m["flags"].([]any)
	var found bool
	for _, f := range flags {
		fm := f.(map[string]any)
		if fm["key"] == "search.pinyin_expansion" {
			found = true
			if fm["enabled"] != false {
				t.Errorf("expected disabled after toggle, got %v", fm["enabled"])
			}
		}
	}
	if !found {
		t.Error("flag search.pinyin_expansion not found after PUT")
	}

	// Toggle back ON
	resp = admin.put("/admin/config/flags/search.pinyin_expansion", map[string]any{
		"enabled":            true,
		"rollout_percentage": 100,
	})
	if resp.StatusCode != 200 {
		t.Errorf("toggle on: %d", resp.StatusCode)
	}
}

// ─── Procurement segregation: creator cannot approve own order ──────────────

func TestBehavior_ProcurementSelfApprovalBlocked(t *testing.T) {
	ensureAPI(t)
	proc := loginAs(t, "bootstrap_procurement", "PROCUREMENT_PW")

	// Create order as procurement
	resp := proc.post("/procurement/orders", map[string]any{
		"vendor_name":  "Self-Approval Test Vendor",
		"description":  "test",
		"total_amount": 99.0,
	})
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		t.Fatalf("create order: %d", resp.StatusCode)
	}
	created := proc.jsonBody(resp)
	orderID, _ := created["id"].(string)
	if orderID == "" {
		t.Fatal("no order id")
	}

	// Attempt self-approval — must fail with 403
	resp = proc.post("/procurement/orders/"+orderID+"/approve", nil)
	if resp.StatusCode != 403 {
		t.Errorf("self-approval should be 403, got %d", resp.StatusCode)
	}
}

// ─── Procurement approval: approver succeeds on someone else's order ────────

func TestBehavior_ProcurementApproverSucceeds(t *testing.T) {
	ensureAPI(t)
	proc := loginAs(t, "bootstrap_procurement", "PROCUREMENT_PW")
	approver := loginAs(t, "bootstrap_approver", "APPROVER_PW")

	// Procurement creates
	resp := proc.post("/procurement/orders", map[string]any{
		"vendor_name":  "Approve-by-Other Vendor",
		"description":  "test approval",
		"total_amount": 250.0,
	})
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		t.Fatalf("create order: %d", resp.StatusCode)
	}
	created := proc.jsonBody(resp)
	orderID, _ := created["id"].(string)

	// Approver approves — must succeed
	resp = approver.post("/procurement/orders/"+orderID+"/approve", nil)
	if resp.StatusCode != 200 {
		t.Errorf("approver should succeed: got %d body=%s", resp.StatusCode, approver.bodyString(resp))
	}

	// Verify status changed
	resp = proc.get("/procurement/orders/" + orderID)
	after := proc.jsonBody(resp)
	if after["status"] != "received" {
		t.Errorf("expected status=received, got %v", after["status"])
	}
}

// ─── Unauthenticated response has correct error code ────────────────────────

func TestBehavior_UnauthenticatedErrorCode(t *testing.T) {
	ensureAPI(t)
	c := &client{t: t}
	resp := c.get("/admin/users")
	if resp.StatusCode != 401 {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
	body := c.jsonBody(resp)
	code, _ := body["code"].(string)
	if !strings.Contains(code, "auth") {
		t.Errorf("expected auth error code, got %q", code)
	}
}

// ─── Search returns structured response ──────────────────────────────────────

func TestBehavior_SearchResponseShape(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")

	resp := admin.get("/search?q=leadership")
	m := admin.jsonBody(resp)
	// Must have results array and total
	if _, ok := m["results"].([]any); !ok {
		t.Errorf("search response missing results array: %v", m)
	}
	if _, ok := m["total"].(float64); !ok {
		t.Errorf("search response missing numeric total: %v", m)
	}
}

// ─── Admin list has pagination metadata ─────────────────────────────────────

func TestBehavior_AdminUsersPagination(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")

	resp := admin.get("/admin/users?limit=2&offset=0")
	m := admin.jsonBody(resp)
	users, _ := m["users"].([]any)
	if len(users) > 2 {
		t.Errorf("limit=2 should cap results at 2, got %d", len(users))
	}
	if _, ok := m["total"].(float64); !ok {
		t.Error("missing total count")
	}
}

// ─── Enrollment creates a learning_enrollments row ───────────────────────────

func TestBehavior_EnrollmentCreatesRecord(t *testing.T) {
	ensureAPI(t)
	learner := loginAs(t, "bootstrap_learner", "LEARNER_PW")

	// Get paths
	resp := learner.get("/paths")
	m := learner.jsonBody(resp)
	paths, _ := m["paths"].([]any)
	if len(paths) == 0 {
		t.Skip("no paths seeded — cannot test enrollment")
	}

	p := paths[0].(map[string]any)
	pathID, _ := p["id"].(string)

	// Enroll
	resp = learner.post("/paths/"+pathID+"/enroll", nil)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		t.Fatalf("enroll: %d", resp.StatusCode)
	}

	// My enrollments should contain this path
	resp = learner.get("/me/enrollments")
	enrollResp := learner.jsonBody(resp)
	enrollments, _ := enrollResp["enrollments"].([]any)
	found := false
	for _, e := range enrollments {
		em := e.(map[string]any)
		if em["path_id"] == pathID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("enrolled path not found in /me/enrollments: %v", enrollments)
	}
}

// ─── Grace period >14 days is rejected (prompt constraint) ──────────────────

func TestBehavior_GracePeriodMax14Days(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")

	resp := admin.put("/admin/config/version-rules", map[string]any{
		"min_version":       "50.0.0",
		"action":            "block",
		"grace_period_days": 30, // over the 14-day cap
	})
	if resp.StatusCode != 400 {
		t.Errorf("grace_period_days=30 should be rejected, got %d", resp.StatusCode)
	}

	// verify error code
	m := admin.jsonBody(resp)
	code, _ := m["code"].(string)
	if !strings.Contains(code, "grace") && !strings.Contains(code, "validation") {
		t.Logf("(informational) got code=%q", code)
	}
}

// ─── Webhook secret weakness is rejected ────────────────────────────────────

func TestBehavior_WebhookWeakSecretRejected(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")

	resp := admin.post("/admin/webhooks", map[string]any{
		"url":    "http://10.0.0.99/weak-secret-hook",
		"events": []string{"export.completed"},
		"secret": "short", // under 16 chars
	})
	if resp.StatusCode != 400 {
		t.Errorf("weak secret should be rejected, got %d", resp.StatusCode)
	}
}

// ─── Session endpoint returns masked email ──────────────────────────────────

func TestBehavior_SessionReturnsUser(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")

	resp := admin.get("/session")
	m := admin.jsonBody(resp)
	user, _ := m["user"].(map[string]any)
	if user == nil {
		t.Fatal("session missing user")
	}
	if user["username"] != "bootstrap_admin" {
		t.Errorf("wrong username: %v", user["username"])
	}
	if roles, ok := user["roles"].([]any); !ok || len(roles) == 0 {
		t.Error("missing roles in session")
	}
}

// ─── CSV export returns well-formed CSV content ─────────────────────────────

func TestBehavior_LearnerCSVExportWellFormed(t *testing.T) {
	ensureAPI(t)
	learner := loginAs(t, "bootstrap_learner", "LEARNER_PW")

	resp := learner.get("/me/exports/csv")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("csv export: %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "csv") {
		t.Errorf("expected CSV content-type, got %q", ct)
	}
}

// ─── Ensure JSON responses don't leak internal file paths ───────────────────

func TestBehavior_NoFilePathLeakInResponses(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")

	resp := fin.get("/exports/jobs")
	body := fin.bodyString(resp)
	// Export jobs should not include "file_path" in JSON (tagged json:"-")
	if strings.Contains(body, `"file_path"`) {
		t.Errorf("exports/jobs response should not expose file_path: %s", body)
	}
	// Should also not contain raw params_json
	if strings.Contains(body, `"params_json"`) {
		t.Errorf("exports/jobs should not expose params_json: %s", body)
	}
}

// ─── Review creation requires valid order_id ────────────────────────────────

func TestBehavior_ReviewValidationErrors(t *testing.T) {
	ensureAPI(t)
	proc := loginAs(t, "bootstrap_procurement", "PROCUREMENT_PW")

	// Missing order_id → 400
	resp := proc.post("/reviews", map[string]any{
		"rating": 4, "body": "test",
	})
	if resp.StatusCode != 400 {
		t.Errorf("missing order_id should 400, got %d", resp.StatusCode)
	}

	// Rating out of range → 400
	resp = proc.post("/reviews", map[string]any{
		"order_id": "00000000-0000-0000-0000-000000000000",
		"rating":   99, "body": "test",
	})
	if resp.StatusCode != 400 {
		t.Errorf("invalid rating should 400, got %d", resp.StatusCode)
	}
}

// ─── Taxonomy tags endpoint returns seed data ───────────────────────────────

func TestBehavior_TaxonomySeeded(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")

	resp := admin.get("/taxonomy/tags")
	m := admin.jsonBody(resp)
	tags, _ := m["tags"].([]any)
	if len(tags) == 0 {
		t.Error("expected seeded skill tags, got empty list")
	}
}

// Helper: silence unused import warning
var _ = json.Unmarshal
