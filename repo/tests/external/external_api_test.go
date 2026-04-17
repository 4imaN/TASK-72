// Package external_test — real HTTP API tests against the live Docker stack.
// No mocks. Every request is a real HTTP call to localhost:8080.
//
// Passwords are deterministic (set in infra/bootstrap/bootstrap-runtime.sh)
// and injected via env vars ADMIN_PW, FINANCE_PW, etc. The run_tests.sh
// script sets these automatically.
//
// Run: ./run_tests.sh --external
// Or manually:
//   export ADMIN_PW="Portal-Admin-2026!" FINANCE_PW="Portal-Finance-2026!" ...
//   go test ./tests/external/... -v -count=1
package external_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const baseURL = "http://localhost:8080"
const apiBase = baseURL + "/api/v1"

// ─── Helpers ─────────────────────────────────────────────────────────────────

type client struct {
	t      *testing.T
	cookie string
}

func (c *client) req(method, path string, body any) *http.Response {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	url := apiBase + path
	if strings.HasPrefix(path, "/api/") {
		url = baseURL + path
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		c.t.Fatalf("build %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cookie != "" {
		req.Header.Set("Cookie", "portal_session="+c.cookie)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func (c *client) get(path string) *http.Response    { return c.req("GET", path, nil) }
func (c *client) post(path string, body any) *http.Response { return c.req("POST", path, body) }
func (c *client) put(path string, body any) *http.Response  { return c.req("PUT", path, body) }

func (c *client) bodyString(resp *http.Response) string {
	c.t.Helper()
	defer resp.Body.Close()
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 512)
	for {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return string(buf)
}

func (c *client) jsonBody(resp *http.Response) map[string]any {
	c.t.Helper()
	defer resp.Body.Close()
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	return m
}

func (c *client) statusOf(resp *http.Response) int {
	resp.Body.Close()
	return resp.StatusCode
}

// sessionCache stores one cookie per username so we don't trigger
// brute-force protection by logging in 40+ times in rapid succession.
var sessionCache = map[string]string{}

func loginAs(t *testing.T, username, envKey string) *client {
	t.Helper()
	// Return cached session if we already logged in as this user
	if cookie, ok := sessionCache[username]; ok {
		return &client{t: t, cookie: cookie}
	}

	pw := os.Getenv(envKey)
	if pw == "" {
		t.Skipf("%s not set — run via ./run_tests.sh --external", envKey)
	}
	c := &client{t: t}
	resp := c.post("/auth/login", map[string]string{
		"username": username, "password": pw,
	})
	for _, ck := range resp.Cookies() {
		if ck.Name == "portal_session" {
			c.cookie = ck.Value
		}
	}
	resp.Body.Close()
	if c.cookie == "" {
		t.Fatalf("login as %s failed: no cookie", username)
	}
	sessionCache[username] = c.cookie
	return c
}

func ensureAPI(t *testing.T) {
	t.Helper()
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get(baseURL + "/api/health")
	if err != nil {
		t.Skip("API not reachable — docker stack not running")
	}
	resp.Body.Close()
}

func assertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	got := resp.StatusCode
	resp.Body.Close()
	if got != want {
		t.Errorf("expected %d, got %d", want, got)
	}
}

// ─── Public endpoints ────────────────────────────────────────────────────────

func TestHealth(t *testing.T) {
	ensureAPI(t)
	c := &client{t: t}
	resp := c.get("/api/health")
	m := c.jsonBody(resp)
	if m["status"] != "ok" {
		t.Errorf("health: %v", m)
	}
}

func TestVersion(t *testing.T) {
	ensureAPI(t)
	c := &client{t: t}
	resp := c.get("/api/version")
	if resp.StatusCode != 200 {
		t.Errorf("version: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// ─── Auth ────────────────────────────────────────────────────────────────────

func TestLoginSuccess(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/session")
	m := admin.jsonBody(resp)
	if m["user"] == nil {
		t.Error("session should return user")
	}
}

func TestLoginBadPassword(t *testing.T) {
	ensureAPI(t)
	c := &client{t: t}
	resp := c.post("/auth/login", map[string]string{
		"username": "bootstrap_admin", "password": "wrong",
	})
	assertStatus(t, resp, 401)
}

func TestUnauthenticatedAccess(t *testing.T) {
	ensureAPI(t)
	c := &client{t: t}
	endpoints := []string{
		"/catalog/resources", "/search", "/paths", "/me/progress",
		"/reconciliation/runs", "/admin/users",
	}
	for _, ep := range endpoints {
		resp := c.get(ep)
		if resp.StatusCode != 401 {
			t.Errorf("GET %s unauthenticated: expected 401, got %d", ep, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestPing(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/ping")
	if resp.StatusCode != 200 {
		t.Errorf("ping: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// ─── Catalog ─────────────────────────────────────────────────────────────────

func TestCatalogCRUD(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")

	// List
	resp := admin.get("/catalog/resources")
	m := admin.jsonBody(resp)
	if m["resources"] == nil {
		t.Error("list catalog: missing resources")
	}

	// Create
	resp = admin.post("/catalog/resources", map[string]any{
		"title": "Test Resource " + fmt.Sprint(time.Now().UnixMilli()),
		"content_type": "article", "category": "engineering", "is_published": true,
	})
	created := admin.jsonBody(resp)
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		t.Fatalf("create resource: %d", resp.StatusCode)
	}
	id, _ := created["id"].(string)

	// Get
	resp = admin.get("/catalog/resources/" + id)
	assertStatus(t, resp, 200)

	// Update
	resp = admin.put("/catalog/resources/"+id, map[string]any{
		"title": "Updated Resource", "is_published": true,
	})
	assertStatus(t, resp, 200)

	// Archive
	resp = admin.post("/catalog/resources/"+id+"/archive", nil)
	assertStatus(t, resp, 200)

	// Restore
	resp = admin.post("/catalog/resources/"+id+"/restore", nil)
	assertStatus(t, resp, 200)
}

func TestCatalogReadPermission(t *testing.T) {
	ensureAPI(t)
	learner := loginAs(t, "bootstrap_learner", "LEARNER_PW")
	// Learner can read
	resp := learner.get("/catalog/resources")
	assertStatus(t, resp, 200)
	// Learner cannot write
	resp = learner.post("/catalog/resources", map[string]any{
		"title": "Forbidden", "content_type": "article", "category": "data",
	})
	assertStatus(t, resp, 403)
}

// ─── Search ──────────────────────────────────────────────────────────────────

func TestSearch(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/search?q=leadership")
	m := admin.jsonBody(resp)
	if m["results"] == nil {
		t.Error("search: missing results")
	}
}

func TestSearchRebuild(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.post("/search/rebuild", nil)
	assertStatus(t, resp, 200)
}

func TestArchiveBuckets(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/archive/buckets?type=month")
	assertStatus(t, resp, 200)
}

func TestArchiveBucketResources(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/archive/buckets/month/2024-01/resources")
	assertStatus(t, resp, 200)
}

// ─── Taxonomy ────────────────────────────────────────────────────────────────

func TestTaxonomyTags(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/taxonomy/tags")
	m := admin.jsonBody(resp)
	if m["tags"] == nil {
		t.Error("missing tags")
	}
}

func TestTaxonomyTagDetail(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/taxonomy/tags")
	m := admin.jsonBody(resp)
	tags, _ := m["tags"].([]any)
	if len(tags) > 0 {
		tag := tags[0].(map[string]any)
		id := fmt.Sprintf("%.0f", tag["id"].(float64))
		resp = admin.get("/taxonomy/tags/" + id)
		assertStatus(t, resp, 200)
	}
}

func TestTaxonomyConflicts(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/taxonomy/conflicts")
	assertStatus(t, resp, 200)
}

func TestTaxonomyAddSynonym(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	// Get first tag
	resp := admin.get("/taxonomy/tags")
	m := admin.jsonBody(resp)
	tags, _ := m["tags"].([]any)
	if len(tags) == 0 {
		t.Skip("no tags to test synonym")
	}
	tag := tags[0].(map[string]any)
	id := fmt.Sprintf("%.0f", tag["id"].(float64))
	resp = admin.post("/taxonomy/tags/"+id+"/synonyms", map[string]string{
		"text": "test_syn_" + fmt.Sprint(time.Now().UnixMilli()), "type": "alias",
	})
	if resp.StatusCode != 200 && resp.StatusCode != 400 { // 400 = conflict is also valid
		t.Errorf("add synonym: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// ─── Learning ────────────────────────────────────────────────────────────────

func TestLearningPaths(t *testing.T) {
	ensureAPI(t)
	learner := loginAs(t, "bootstrap_learner", "LEARNER_PW")
	resp := learner.get("/paths")
	assertStatus(t, resp, 200)
}

func TestLearningPathDetail(t *testing.T) {
	ensureAPI(t)
	learner := loginAs(t, "bootstrap_learner", "LEARNER_PW")
	resp := learner.get("/paths")
	m := learner.jsonBody(resp)
	paths, _ := m["paths"].([]any)
	if len(paths) > 0 {
		p := paths[0].(map[string]any)
		id, _ := p["id"].(string)
		resp = learner.get("/paths/" + id)
		assertStatus(t, resp, 200)
	}
}

func TestLearningEnroll(t *testing.T) {
	ensureAPI(t)
	learner := loginAs(t, "bootstrap_learner", "LEARNER_PW")
	resp := learner.get("/paths")
	m := learner.jsonBody(resp)
	paths, _ := m["paths"].([]any)
	if len(paths) > 0 {
		p := paths[0].(map[string]any)
		id, _ := p["id"].(string)
		resp = learner.post("/paths/"+id+"/enroll", nil)
		if resp.StatusCode != 200 && resp.StatusCode != 201 {
			t.Errorf("enroll: %d", resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestLearningPathProgress(t *testing.T) {
	ensureAPI(t)
	learner := loginAs(t, "bootstrap_learner", "LEARNER_PW")
	resp := learner.get("/paths")
	m := learner.jsonBody(resp)
	paths, _ := m["paths"].([]any)
	if len(paths) > 0 {
		p := paths[0].(map[string]any)
		id, _ := p["id"].(string)
		resp = learner.get("/paths/" + id + "/progress")
		assertStatus(t, resp, 200)
	}
}

func TestMeEnrollments(t *testing.T) {
	ensureAPI(t)
	learner := loginAs(t, "bootstrap_learner", "LEARNER_PW")
	resp := learner.get("/me/enrollments")
	assertStatus(t, resp, 200)
}

func TestMeProgress(t *testing.T) {
	ensureAPI(t)
	learner := loginAs(t, "bootstrap_learner", "LEARNER_PW")
	resp := learner.get("/me/progress")
	assertStatus(t, resp, 200)
}

func TestMeExportsCSV(t *testing.T) {
	ensureAPI(t)
	learner := loginAs(t, "bootstrap_learner", "LEARNER_PW")
	resp := learner.get("/me/exports/csv")
	if resp.StatusCode != 200 {
		t.Errorf("csv export: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// ─── Recommendations ─────────────────────────────────────────────────────────

func TestRecommendations(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/recommendations")
	assertStatus(t, resp, 200)
}

func TestRecommendationEvent(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.post("/recommendations/events", map[string]string{
		"resource_id": "00000000-0000-0000-0000-000000000000", "event_type": "view",
	})
	if resp.StatusCode != 204 && resp.StatusCode != 200 {
		t.Errorf("rec event: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// ─── Reviews & Appeals ───────────────────────────────────────────────────────

func TestReviewsList(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	// Need an order to list reviews for — use a fake order ID; should return empty
	resp := admin.get("/orders/00000000-0000-0000-0000-000000000000/reviews")
	assertStatus(t, resp, 200)
}

func TestAppealsList(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/appeals")
	assertStatus(t, resp, 200)
}

func TestModerationQueue(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/moderation/queue")
	assertStatus(t, resp, 200)
}

// ─── Reconciliation & Settlements ────────────────────────────────────────────

func TestReconciliationStatements(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.get("/reconciliation/statements")
	assertStatus(t, resp, 200)
}

func TestReconciliationRules(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.get("/reconciliation/rules")
	assertStatus(t, resp, 200)
}

func TestReconciliationRuns(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.get("/reconciliation/runs")
	assertStatus(t, resp, 200)
}

func TestReconciliationBatches(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.get("/reconciliation/batches")
	assertStatus(t, resp, 200)
}

func TestReconciliationRunCreate(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.post("/reconciliation/runs", map[string]string{"period": "2020-01"})
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		t.Errorf("create run: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestReconciliationStatementsImport(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.post("/reconciliation/statements", map[string]any{
		"source_file": "test_external.csv",
		"checksum":    "ext123",
		"rows": []map[string]any{{
			"line_description": "External test row",
			"statement_amount": 10000,
			"transaction_date": "2025-01-15",
		}},
	})
	if resp.StatusCode != 201 {
		t.Errorf("import statements: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestReconciliationVariances(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	// Get a run ID first
	resp := fin.get("/reconciliation/runs")
	m := fin.jsonBody(resp)
	runs, _ := m["runs"].([]any)
	if len(runs) > 0 {
		run := runs[0].(map[string]any)
		id, _ := run["id"].(string)
		resp = fin.get("/reconciliation/runs/" + id + "/variances")
		assertStatus(t, resp, 200)
	}
}

// ─── Exports ─────────────────────────────────────────────────────────────────

func TestExportJobsList(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.get("/exports/jobs")
	assertStatus(t, resp, 200)
}

func TestExportJobCreate(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.post("/exports/jobs", map[string]any{
		"type": "reconciliation_export", "params": map[string]any{},
	})
	if resp.StatusCode != 201 {
		t.Errorf("create export job: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// ─── Config Center ───────────────────────────────────────────────────────────

func TestConfigFlags(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/admin/config/flags")
	m := admin.jsonBody(resp)
	if m["flags"] == nil {
		t.Error("missing flags")
	}
}

func TestConfigFlagSet(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.put("/admin/config/flags/mfa.enabled", map[string]any{
		"enabled": true, "rollout_percentage": 100,
	})
	assertStatus(t, resp, 200)
}

func TestConfigParams(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/admin/config/params")
	assertStatus(t, resp, 200)
}

func TestConfigParamSet(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.put("/admin/config/params/session.idle_timeout_seconds", map[string]any{
		"value": "900",
	})
	assertStatus(t, resp, 200)
}

func TestConfigVersionRules(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/admin/config/version-rules")
	assertStatus(t, resp, 200)
}

func TestConfigVersionRuleSet(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	ver := fmt.Sprintf("99.%d.0", time.Now().UnixMilli()%10000)
	resp := admin.put("/admin/config/version-rules", map[string]any{
		"min_version": ver, "action": "warn", "message": "test",
	})
	assertStatus(t, resp, 200)
}

// ─── Webhooks ────────────────────────────────────────────────────────────────

func TestWebhooksList(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/admin/webhooks")
	assertStatus(t, resp, 200)
}

func TestWebhookCreate(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.post("/admin/webhooks", map[string]any{
		"url": "http://10.0.0.1/test-hook", "events": []string{"export.completed"},
	})
	if resp.StatusCode != 201 {
		t.Errorf("create webhook: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestWebhookCreateRejectsPublicIP(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.post("/admin/webhooks", map[string]any{
		"url": "http://8.8.8.8/exfil", "events": []string{"export.completed"},
	})
	assertStatus(t, resp, 400)
}

func TestWebhookDeliveries(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/admin/webhooks/deliveries")
	assertStatus(t, resp, 200)
}

func TestWebhookProcess(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.post("/admin/webhooks/process", nil)
	assertStatus(t, resp, 200)
}

// ─── Admin Users ─────────────────────────────────────────────────────────────

func TestAdminUsersList(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/admin/users")
	m := admin.jsonBody(resp)
	if m["users"] == nil {
		t.Error("missing users")
	}
}

func TestAdminUserDetail(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/admin/users")
	m := admin.jsonBody(resp)
	users, _ := m["users"].([]any)
	if len(users) > 0 {
		u := users[0].(map[string]any)
		id, _ := u["id"].(string)
		resp = admin.get("/admin/users/" + id)
		assertStatus(t, resp, 200)
	}
}

func TestAdminUserRevealEmail(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/admin/users")
	m := admin.jsonBody(resp)
	users, _ := m["users"].([]any)
	if len(users) > 0 {
		u := users[0].(map[string]any)
		id, _ := u["id"].(string)
		resp = admin.get("/admin/users/" + id + "/reveal-email")
		assertStatus(t, resp, 200)
	}
}

func TestAdminUserRolesUpdate(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	// Get learner user ID
	resp := admin.get("/admin/users")
	m := admin.jsonBody(resp)
	users, _ := m["users"].([]any)
	for _, u := range users {
		um := u.(map[string]any)
		if um["username"] == "bootstrap_learner" {
			id, _ := um["id"].(string)
			resp = admin.put("/admin/users/"+id+"/roles", map[string]any{
				"roles": []string{"learner"},
			})
			assertStatus(t, resp, 200)
			break
		}
	}
}

// ─── Audit ───────────────────────────────────────────────────────────────────

func TestAuditLog(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/admin/audit")
	// Must be 200. The "tolerate 500" version masked a real schema mismatch
	// (audit_logs uses occurred_at, not created_at); that bug is now fixed
	// so any 500 here is a real regression.
	if resp.StatusCode != 200 {
		resp.Body.Close()
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := admin.jsonBody(resp)
	if _, ok := body["events"]; !ok {
		t.Errorf("audit log response missing 'events' key: %v", body)
	}
}

// ─── Procurement ─────────────────────────────────────────────────────────────

func TestProcurementOrders(t *testing.T) {
	ensureAPI(t)
	proc := loginAs(t, "bootstrap_procurement", "PROCUREMENT_PW")
	resp := proc.get("/procurement/orders")
	assertStatus(t, resp, 200)
}

func TestProcurementOrderCreate(t *testing.T) {
	ensureAPI(t)
	proc := loginAs(t, "bootstrap_procurement", "PROCUREMENT_PW")
	resp := proc.post("/procurement/orders", map[string]any{
		"vendor_name": "External Test Vendor", "description": "test", "total_amount": 100.0,
	})
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		t.Errorf("create order: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestProcurementOrderDetail(t *testing.T) {
	ensureAPI(t)
	proc := loginAs(t, "bootstrap_procurement", "PROCUREMENT_PW")
	resp := proc.get("/procurement/orders")
	m := proc.jsonBody(resp)
	orders, _ := m["orders"].([]any)
	if len(orders) > 0 {
		o := orders[0].(map[string]any)
		id, _ := o["id"].(string)
		resp = proc.get("/procurement/orders/" + id)
		assertStatus(t, resp, 200)
	}
}

func TestProcurementApproveRequiresPermission(t *testing.T) {
	ensureAPI(t)
	proc := loginAs(t, "bootstrap_procurement", "PROCUREMENT_PW")
	// Procurement doesn't have orders:approve
	resp := proc.post("/procurement/orders/00000000-0000-0000-0000-000000000000/approve", nil)
	assertStatus(t, resp, 403)
}

func TestProcurementRejectRequiresPermission(t *testing.T) {
	ensureAPI(t)
	proc := loginAs(t, "bootstrap_procurement", "PROCUREMENT_PW")
	resp := proc.post("/procurement/orders/00000000-0000-0000-0000-000000000000/reject",
		map[string]string{"reason": "test"})
	assertStatus(t, resp, 403)
}

// ─── MFA endpoints (test that they exist and respond) ────────────────────────

func TestMFAEndpoints(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	// Exercise each MFA route with a plainly-wrong payload and assert it
	// responds with a classified error (200 for enroll-start, 4xx otherwise),
	// never 404/405/500. The old "not 404" check tolerated 500s.
	cases := []struct {
		path       string
		okStatuses []int
	}{
		{"/mfa/enroll/start", []int{200}},
		{"/mfa/verify", []int{400, 401, 403}},
		{"/mfa/recovery", []int{400, 401, 403}},
		{"/auth/mfa/verify", []int{400, 401}},
		{"/auth/mfa/recovery", []int{400, 401}},
	}
	for _, tc := range cases {
		resp := admin.post(tc.path, map[string]string{"code": "000000"})
		got := resp.StatusCode
		resp.Body.Close()
		ok := false
		for _, s := range tc.okStatuses {
			if got == s {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("POST %s: got %d, expected one of %v", tc.path, got, tc.okStatuses)
		}
	}
}

// ─── Password change ─────────────────────────────────────────────────────────

func TestPasswordChangeEndpoint(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	// Send a request with missing new_password — should return 400, and importantly
	// must NOT mutate any user state. Using current_password=wrong would actually
	// rotate the password for bootstrap users (force_password_reset=true skips the
	// current-password check), which would break all subsequent tests.
	resp := admin.post("/auth/password/change", map[string]string{
		"current_password": "anything", "new_password": "",
	})
	if resp.StatusCode == 404 {
		t.Error("password change: unexpected 404")
	}
	if resp.StatusCode != 400 {
		t.Errorf("password change w/ empty new_password: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// ─── RBAC: learner cannot access admin ───────────────────────────────────────

func TestLearnerCannotAccessAdmin(t *testing.T) {
	ensureAPI(t)
	learner := loginAs(t, "bootstrap_learner", "LEARNER_PW")
	adminPaths := []string{
		"/admin/users", "/admin/config/flags", "/admin/webhooks", "/admin/audit",
	}
	for _, p := range adminPaths {
		resp := learner.get(p)
		if resp.StatusCode != 403 {
			t.Errorf("GET %s as learner: expected 403, got %d", p, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestApproverCanReadRecon(t *testing.T) {
	ensureAPI(t)
	approver := loginAs(t, "bootstrap_approver", "APPROVER_PW")
	resp := approver.get("/reconciliation/runs")
	assertStatus(t, resp, 200)
}

func TestApproverCannotWriteRecon(t *testing.T) {
	ensureAPI(t)
	approver := loginAs(t, "bootstrap_approver", "APPROVER_PW")
	resp := approver.post("/reconciliation/runs", map[string]string{"period": "2020-06"})
	assertStatus(t, resp, 403)
}

// ─── Session endpoint ────────────────────────────────────────────────────────

func TestSessionEndpoint(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/session")
	m := admin.jsonBody(resp)
	if m["user"] == nil {
		t.Error("session missing user")
	}
}

// ─── Run detail ──────────────────────────────────────────────────────────────

func TestReconciliationRunDetail(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.get("/reconciliation/runs")
	m := fin.jsonBody(resp)
	runs, _ := m["runs"].([]any)
	if len(runs) > 0 {
		run := runs[0].(map[string]any)
		id, _ := run["id"].(string)
		resp = fin.get("/reconciliation/runs/" + id)
		assertStatus(t, resp, 200)
	}
}

// ─── Additional coverage for remaining endpoints ────────────────────────────

func TestRecordProgress(t *testing.T) {
	ensureAPI(t)
	learner := loginAs(t, "bootstrap_learner", "LEARNER_PW")
	resp := learner.post("/me/progress/00000000-0000-0000-0000-000000000000", map[string]any{
		"event_type": "progress", "progress_pct": 50.0,
	})
	// Fake resource_id: backend rejects with 400 (validation) or 422 (unknown
	// resource). Must never be 404 (route missing) or 500.
	if resp.StatusCode != 400 && resp.StatusCode != 422 {
		t.Errorf("progress on fake resource: expected 400/422, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestReviewCreateRequiresData(t *testing.T) {
	ensureAPI(t)
	proc := loginAs(t, "bootstrap_procurement", "PROCUREMENT_PW")
	resp := proc.post("/reviews", map[string]any{
		"order_id": "", "rating": 3, "body": "test",
	})
	assertStatus(t, resp, 400) // missing order_id
}

func TestReviewFlagEndpoint(t *testing.T) {
	ensureAPI(t)
	// reviews:write is required — use procurement user
	proc := loginAs(t, "bootstrap_procurement", "PROCUREMENT_PW")
	resp := proc.post("/reviews/00000000-0000-0000-0000-000000000000/flag", map[string]string{
		"reason": "test flag",
	})
	// Fake UUID → review not found. Must surface as 404, never 405 or 500.
	if resp.StatusCode != 404 {
		t.Errorf("flag on missing review: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestReviewReplyEndpoint(t *testing.T) {
	ensureAPI(t)
	proc := loginAs(t, "bootstrap_procurement", "PROCUREMENT_PW")
	resp := proc.post("/reviews/00000000-0000-0000-0000-000000000000/reply", map[string]string{
		"reply_text": "test reply",
	})
	// Non-existent review → 403 (not own order) or 404. Never 405/500.
	if resp.StatusCode != 403 && resp.StatusCode != 404 {
		t.Errorf("reply on missing review: expected 403/404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestReviewDetailEndpoint(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/reviews/00000000-0000-0000-0000-000000000000")
	if resp.StatusCode != 404 {
		t.Errorf("review detail missing id: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestReviewAttachmentEndpoint(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/reviews/attachments/00000000-0000-0000-0000-000000000000")
	if resp.StatusCode != 404 {
		t.Errorf("attachment missing id: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAppealCreateEndpoint(t *testing.T) {
	ensureAPI(t)
	proc := loginAs(t, "bootstrap_procurement", "PROCUREMENT_PW")
	resp := proc.post("/appeals", map[string]any{
		"review_id": "00000000-0000-0000-0000-000000000000", "reason": "test",
	})
	// Fake review_id → validation/lookup error. Must be 400/404, never 500.
	if resp.StatusCode != 400 && resp.StatusCode != 404 {
		t.Errorf("appeal create fake id: expected 400/404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAppealDetailEndpoint(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/appeals/00000000-0000-0000-0000-000000000000")
	if resp.StatusCode != 404 {
		t.Errorf("appeal detail missing id: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAppealArbitrateEndpoint(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.post("/appeals/00000000-0000-0000-0000-000000000000/arbitrate", map[string]string{
		"outcome": "restore",
	})
	// Fake appeal_id → 404. Never 405/500.
	if resp.StatusCode != 404 {
		t.Errorf("arbitrate missing id: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestEvidenceDownloadEndpoint(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/appeals/evidence/00000000-0000-0000-0000-000000000000")
	if resp.StatusCode != 404 {
		t.Errorf("evidence download missing id: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestModerationDecideEndpoint(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.post("/moderation/queue/00000000-0000-0000-0000-000000000000/decide", map[string]string{
		"decision": "approve",
	})
	if resp.StatusCode != 404 {
		t.Errorf("moderation decide missing id: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSettlementBatchCreate(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	// Need a valid run_id
	resp := fin.get("/reconciliation/runs")
	m := fin.jsonBody(resp)
	runs, _ := m["runs"].([]any)
	if len(runs) > 0 {
		run := runs[0].(map[string]any)
		runID, _ := run["id"].(string)
		resp = fin.post("/reconciliation/batches", map[string]any{
			"run_id": runID,
			"lines":  []map[string]any{{"amount": 1000, "direction": "AP", "cost_center_id": "CC-1"}},
		})
		if resp.StatusCode == 405 {
			t.Error("batch create returns expected error for fake ID (not 405 method-not-allowed)")
		}
		resp.Body.Close()
	}
}

func TestSettlementBatchDetail(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.get("/reconciliation/batches/00000000-0000-0000-0000-000000000000")
	// Fake UUID → 404 (not found) is the correct behavior
	body := fin.jsonBody(resp)
	if resp.StatusCode != 404 {
		t.Errorf("batch detail with fake ID: expected 404, got %d", resp.StatusCode)
	}
	if code, _ := body["code"].(string); code == "" {
		t.Error("404 response should include an error code")
	}
}

func TestVarianceSubmitApproval(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.post("/reconciliation/variances/00000000-0000-0000-0000-000000000000/submit-approval", nil)
	body := fin.jsonBody(resp)
	if resp.StatusCode != 409 {
		t.Errorf("submit-approval on fake variance: expected 409, got %d", resp.StatusCode)
	}
	if code, _ := body["code"].(string); code != "reconciliation.invalid_state" {
		t.Errorf("expected code=reconciliation.invalid_state, got %q", code)
	}
}

func TestVarianceApprove(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.post("/reconciliation/variances/00000000-0000-0000-0000-000000000000/approve", nil)
	body := fin.jsonBody(resp)
	if resp.StatusCode != 409 {
		t.Errorf("approve on fake variance: expected 409, got %d", resp.StatusCode)
	}
	if code, _ := body["code"].(string); code != "reconciliation.invalid_state" {
		t.Errorf("expected code=reconciliation.invalid_state, got %q", code)
	}
}

func TestVarianceApply(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.post("/reconciliation/variances/00000000-0000-0000-0000-000000000000/apply", nil)
	body := fin.jsonBody(resp)
	if resp.StatusCode != 409 {
		t.Errorf("apply on fake variance: expected 409, got %d", resp.StatusCode)
	}
	if code, _ := body["code"].(string); code != "reconciliation.invalid_state" {
		t.Errorf("expected code=reconciliation.invalid_state, got %q", code)
	}
}

func TestBatchSubmit(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.post("/reconciliation/batches/00000000-0000-0000-0000-000000000000/submit", nil)
	body := fin.jsonBody(resp)
	if resp.StatusCode != 409 {
		t.Errorf("submit on fake batch: expected 409, got %d", resp.StatusCode)
	}
	if code, _ := body["code"].(string); code != "reconciliation.invalid_state" {
		t.Errorf("expected code=reconciliation.invalid_state, got %q", code)
	}
}

func TestBatchApprove(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.post("/reconciliation/batches/00000000-0000-0000-0000-000000000000/approve", nil)
	body := fin.jsonBody(resp)
	if resp.StatusCode != 409 {
		t.Errorf("approve on fake batch: expected 409, got %d", resp.StatusCode)
	}
	if code, _ := body["code"].(string); code != "reconciliation.invalid_state" {
		t.Errorf("expected code=reconciliation.invalid_state, got %q", code)
	}
}

func TestBatchExport(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.post("/reconciliation/batches/00000000-0000-0000-0000-000000000000/export", nil)
	body := fin.jsonBody(resp)
	if resp.StatusCode != 409 {
		t.Errorf("export on fake batch: expected 409, got %d", resp.StatusCode)
	}
	if code, _ := body["code"].(string); code != "reconciliation.invalid_state" {
		t.Errorf("expected code=reconciliation.invalid_state, got %q", code)
	}
}

func TestBatchSettle(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.post("/reconciliation/batches/00000000-0000-0000-0000-000000000000/settle", nil)
	body := fin.jsonBody(resp)
	if resp.StatusCode != 409 {
		t.Errorf("settle on fake batch: expected 409, got %d", resp.StatusCode)
	}
	if code, _ := body["code"].(string); code != "reconciliation.invalid_state" {
		t.Errorf("expected code=reconciliation.invalid_state, got %q", code)
	}
}

func TestBatchVoid(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.post("/reconciliation/batches/00000000-0000-0000-0000-000000000000/void", map[string]string{
		"reason": "test void",
	})
	body := fin.jsonBody(resp)
	if resp.StatusCode != 409 {
		t.Errorf("void on fake batch: expected 409, got %d", resp.StatusCode)
	}
	if code, _ := body["code"].(string); code != "reconciliation.invalid_state" {
		t.Errorf("expected code=reconciliation.invalid_state, got %q", code)
	}
}

func TestExportJobDetail(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.get("/exports/jobs/00000000-0000-0000-0000-000000000000")
	// Fake job → 404
	if resp.StatusCode != 404 {
		t.Errorf("export job with fake ID: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestExportJobDownload(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.get("/exports/jobs/00000000-0000-0000-0000-000000000000/download")
	if resp.StatusCode != 404 {
		t.Errorf("export download missing id: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestTaxonomyConflictResolve(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.post("/taxonomy/conflicts/999999/resolve", map[string]string{
		"resolution": "merged",
	})
	// Non-existent conflict → 404 or validation 400. Never 405/500.
	if resp.StatusCode != 400 && resp.StatusCode != 404 {
		t.Errorf("conflict resolve missing id: expected 400/404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestMFAEnrollStart(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.post("/mfa/enroll/start", nil)
	// Admin always allowed to start enrollment → 200 (returns TOTP secret + QR).
	if resp.StatusCode != 200 {
		t.Errorf("mfa enroll start: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestMFAEnrollConfirm(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.post("/mfa/enroll/confirm", map[string]string{"code": "000000"})
	// Admin has no pending enrollment OR code is wrong → 400/401. Never 404/405/500.
	if resp.StatusCode != 400 && resp.StatusCode != 401 {
		t.Errorf("mfa enroll confirm w/ invalid code: expected 400/401, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestReconciliationProcessRun(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.post("/reconciliation/runs/00000000-0000-0000-0000-000000000000/process", nil)
	// Fake run_id → 404 (not found). Never 405/500.
	if resp.StatusCode != 404 {
		t.Errorf("process run missing id: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// ─── Gap coverage: #1 GET /orders/:order_id/reviews ─────────────────────────

func TestOrderReviewsEndpoint(t *testing.T) {
	ensureAPI(t)
	proc := loginAs(t, "bootstrap_procurement", "PROCUREMENT_PW")
	// Create an order first so we have a real ID
	resp := proc.post("/procurement/orders", map[string]any{
		"vendor_name": "Review Test Vendor", "description": "test", "total_amount": 50.0,
	})
	m := proc.jsonBody(resp)
	orderID, _ := m["id"].(string)
	if orderID == "" {
		t.Skip("could not create order")
	}
	// GET reviews for that order — should be 200 with empty list
	resp = proc.get("/orders/" + orderID + "/reviews")
	body := proc.jsonBody(resp)
	if resp.StatusCode != 200 {
		t.Errorf("GET /orders/:id/reviews: expected 200, got %d", resp.StatusCode)
	}
	if body["reviews"] == nil {
		t.Error("response missing 'reviews' key")
	}
}

// ─── Gap coverage: #2 POST /auth/logout ─────────────────────────────────────

func TestAuthLogout(t *testing.T) {
	ensureAPI(t)
	pw := os.Getenv("ADMIN_PW")
	if pw == "" {
		t.Skip("ADMIN_PW not set")
	}
	// Login fresh (don't use cache — we need to invalidate this specific session)
	c := &client{t: t}
	resp := c.post("/auth/login", map[string]string{
		"username": "bootstrap_admin", "password": pw,
	})
	for _, ck := range resp.Cookies() {
		if ck.Name == "portal_session" {
			c.cookie = ck.Value
		}
	}
	resp.Body.Close()
	if c.cookie == "" {
		t.Fatal("login failed")
	}
	// Session should work
	resp = c.get("/session")
	if resp.StatusCode != 200 {
		t.Fatalf("session before logout: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Logout
	resp = c.post("/auth/logout", nil)
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		t.Errorf("POST /auth/logout: expected 200/204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Session should now fail
	resp = c.get("/session")
	if resp.StatusCode == 200 {
		resp.Body.Close()
		t.Error("session should be invalid after logout")
	} else {
		resp.Body.Close()
	}
}

// ─── Gap coverage: #6 Strengthen assertion quality ──────────────────────────

func TestUnauthenticatedAccessResponseBody(t *testing.T) {
	ensureAPI(t)
	c := &client{t: t}
	resp := c.get("/catalog/resources")
	body := c.jsonBody(resp)
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	code, _ := body["code"].(string)
	msg, _ := body["message"].(string)
	if code == "" && msg == "" {
		t.Error("401 response should include code or message")
	}
}

func TestLearnerForbiddenResponseBody(t *testing.T) {
	ensureAPI(t)
	learner := loginAs(t, "bootstrap_learner", "LEARNER_PW")
	resp := learner.get("/admin/users")
	body := learner.jsonBody(resp)
	if resp.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	msg, _ := body["message"].(string)
	if msg == "" {
		t.Error("403 response should include a message")
	}
}

func TestSearchResponseShape(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/search?q=leadership")
	body := admin.jsonBody(resp)
	if body["results"] == nil {
		t.Error("search response missing 'results'")
	}
	if body["total"] == nil {
		t.Error("search response missing 'total'")
	}
	if body["limit"] == nil {
		t.Error("search response missing 'limit'")
	}
}

func TestRecommendationsResponseShape(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/recommendations?limit=3")
	body := admin.jsonBody(resp)
	if body["items"] == nil {
		t.Error("recommendations response missing 'items'")
	}
}

func TestConfigFlagsResponseShape(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/admin/config/flags")
	body := admin.jsonBody(resp)
	flags, _ := body["flags"].([]any)
	if len(flags) == 0 {
		t.Error("expected at least one seeded flag")
	}
	first, _ := flags[0].(map[string]any)
	if first["key"] == nil {
		t.Error("flag missing 'key'")
	}
	if first["enabled"] == nil {
		t.Error("flag missing 'enabled'")
	}
}

func TestAdminUsersResponseShape(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/admin/users")
	body := admin.jsonBody(resp)
	users, _ := body["users"].([]any)
	if len(users) == 0 {
		t.Fatal("expected at least one user")
	}
	u, _ := users[0].(map[string]any)
	for _, key := range []string{"id", "username", "email", "roles"} {
		if u[key] == nil {
			t.Errorf("user response missing '%s'", key)
		}
	}
	// Email should be masked (not contain @)
	email, _ := u["email"].(string)
	if strings.Contains(email, "@") && !strings.Contains(email, "***") {
		t.Error("email should be masked in user list response")
	}
}

func TestProcurementOrderResponseShape(t *testing.T) {
	ensureAPI(t)
	proc := loginAs(t, "bootstrap_procurement", "PROCUREMENT_PW")
	resp := proc.get("/procurement/orders?limit=1")
	body := proc.jsonBody(resp)
	if body["orders"] == nil {
		t.Error("missing 'orders'")
	}
	if body["total"] == nil {
		t.Error("missing 'total'")
	}
}

// ─── Alias routes for full coverage ─────────────────────────────────────────

func TestMFAAliasVerify(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.post("/auth/mfa/verify", map[string]string{"code": "000000"})
	body := admin.jsonBody(resp)
	// Should return 400/403 (bad code or not enrolled) — NOT 404 (route missing)
	if resp.StatusCode == 404 {
		t.Fatal("/auth/mfa/verify route does not exist")
	}
	if msg, _ := body["message"].(string); msg == "" && body["code"] == nil {
		t.Error("MFA verify should return an error body with code or message")
	}
}

func TestMFAAliasRecovery(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.post("/auth/mfa/recovery", map[string]string{"code": "000000"})
	if resp.StatusCode == 404 {
		t.Fatal("/auth/mfa/recovery route does not exist")
	}
	resp.Body.Close()
}

func TestMFAVerifyDirect(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.post("/mfa/verify", map[string]string{"code": "000000"})
	if resp.StatusCode == 404 {
		t.Fatal("/mfa/verify route does not exist")
	}
	resp.Body.Close()
}

func TestMFARecoveryDirect(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.post("/mfa/recovery", map[string]string{"code": "000000"})
	if resp.StatusCode == 404 {
		t.Fatal("/mfa/recovery route does not exist")
	}
	resp.Body.Close()
}

func TestVarianceSubmitApprovalExternal(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.post("/reconciliation/variances/00000000-0000-0000-0000-000000000001/submit-approval", nil)
	if resp.StatusCode != 409 {
		t.Errorf("submit-approval on fake variance: expected 409, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestVarianceApproveExternal(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.post("/reconciliation/variances/00000000-0000-0000-0000-000000000001/approve", nil)
	if resp.StatusCode != 409 {
		t.Errorf("approve on fake variance: expected 409, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestVarianceApplyExternal(t *testing.T) {
	ensureAPI(t)
	fin := loginAs(t, "bootstrap_finance", "FINANCE_PW")
	resp := fin.post("/reconciliation/variances/00000000-0000-0000-0000-000000000001/apply", nil)
	if resp.StatusCode != 409 {
		t.Errorf("apply on fake variance: expected 409, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestArchiveBucketResourcesExternal(t *testing.T) {
	ensureAPI(t)
	admin := loginAs(t, "bootstrap_admin", "ADMIN_PW")
	resp := admin.get("/archive/buckets/tag/nonexistent/resources")
	body := admin.jsonBody(resp)
	// Nonexistent bucket → 200 with empty resources list (not 404)
	if resp.StatusCode != 200 {
		t.Errorf("archive bucket resources: expected 200, got %d", resp.StatusCode)
	}
	resources, _ := body["resources"].([]any)
	if len(resources) != 0 {
		t.Errorf("expected empty resources for nonexistent bucket, got %d", len(resources))
	}
}
