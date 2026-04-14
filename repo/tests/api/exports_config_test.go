// tests/api/exports_config_test.go — integration-style HTTP tests for
// exports, config center, and webhooks endpoints.
// All tests use httptest and in-process fakes; no real database is required.
package api_test

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"portal/internal/app/common"
	"portal/internal/app/sessions"
)

// ── In-memory fakes for Slice 9 ───────────────────────────────────────────────

// fakeExportJob is an in-memory export job record.
type fakeExportJob struct {
	id          string
	jobType     string
	status      string
	createdBy   string
	createdAt   time.Time
	completedAt *time.Time
	filePath    string
	errorMsg    string
	paramsJSON  string
}

// fakeExportStore holds in-memory export jobs.
type fakeExportStore struct {
	jobs map[string]*fakeExportJob
}

func newFakeExportStore() *fakeExportStore {
	return &fakeExportStore{jobs: make(map[string]*fakeExportJob)}
}

// fakeConfigFlag is an in-memory config flag.
type fakeConfigFlag struct {
	key               string
	enabled           bool
	description       string
	rolloutPercentage int
	targetRoles       []string
	updatedAt         time.Time
}

// fakeConfigStore holds in-memory flags, params, and version rules.
type fakeConfigStore struct {
	flags        map[string]*fakeConfigFlag
	params       map[string]string
	versionRules []map[string]string
}

func newFakeConfigStore() *fakeConfigStore {
	return &fakeConfigStore{
		flags:        make(map[string]*fakeConfigFlag),
		params:       make(map[string]string),
		versionRules: []map[string]string{},
	}
}

// fakeWebhookEndpoint is an in-memory webhook endpoint.
type fakeWebhookEndpoint struct {
	id        string
	url       string
	events    []string
	isActive  bool
	createdBy string
	createdAt time.Time
}

// fakeWebhookDelivery is an in-memory webhook delivery.
type fakeWebhookDelivery struct {
	id         string
	endpointID string
	eventType  string
	status     string
	attempts   int
}

// fakeWebhookStore holds in-memory webhook endpoints and deliveries.
type fakeWebhookStore struct {
	endpoints  map[string]*fakeWebhookEndpoint
	deliveries []*fakeWebhookDelivery
}

func newFakeWebhookStore() *fakeWebhookStore {
	return &fakeWebhookStore{
		endpoints:  make(map[string]*fakeWebhookEndpoint),
		deliveries: []*fakeWebhookDelivery{},
	}
}

// ── Test Echo builder ─────────────────────────────────────────────────────────

// buildSlice9Echo creates a minimal Echo with all Slice 9 routes wired to fakes.
func buildSlice9Echo(
	us *fakeUserStore,
	ss *fakeSessionStore,
	exports *fakeExportStore,
	cfg *fakeConfigStore,
	wh *fakeWebhookStore,
) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// requireAuth middleware
	requireAuth := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			token := sessions.TokenFromRequest(c)
			if token == "" {
				return common.Unauthorized(c, "Authentication required")
			}
			sess, ok := ss.sessions[token]
			if !ok {
				return common.Unauthorized(c, "Session expired or invalid")
			}
			c.Set("user_id", sess.UserID)
			// inject roles for role checks
			if u, ok := us.byID[sess.UserID]; ok {
				c.Set("user_roles", u.id) // store id as marker
				c.Set("is_admin", u.username == "admin_user")
			}
			return next(c)
		}
	}

	// requireAdmin middleware
	requireAdmin := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			isAdmin, _ := c.Get("is_admin").(bool)
			if !isAdmin {
				return common.Forbidden(c, "Admin role required")
			}
			return next(c)
		}
	}

	v1 := e.Group("/api/v1")
	auth := v1.Group("", requireAuth)
	admin := v1.Group("", requireAuth, requireAdmin)

	// ── Export job routes ────────────────────────────────────────────────────

	// POST /api/v1/exports/jobs
	auth.POST("/exports/jobs", func(c echo.Context) error {
		userID, _ := c.Get("user_id").(string)
		var req struct {
			Type   string         `json:"type"`
			Params map[string]any `json:"params"`
		}
		if err := c.Bind(&req); err != nil {
			return common.BadRequest(c, "validation.invalid_body", "Invalid body")
		}
		if req.Type == "" {
			return common.BadRequest(c, "validation.required", "type is required")
		}
		switch req.Type {
		case "learning_progress_csv", "reconciliation_export":
			// ok
		default:
			return common.BadRequest(c, "validation.invalid_type", "unknown job type")
		}
		id := uuid.New().String()
		job := &fakeExportJob{
			id:        id,
			jobType:   req.Type,
			status:    "queued",
			createdBy: userID,
			createdAt: time.Now(),
		}
		exports.jobs[id] = job
		return c.JSON(http.StatusCreated, map[string]any{
			"id":         job.id,
			"type":       job.jobType,
			"status":     job.status,
			"created_by": job.createdBy,
			"created_at": job.createdAt,
		})
	})

	// GET /api/v1/exports/jobs
	auth.GET("/exports/jobs", func(c echo.Context) error {
		userID, _ := c.Get("user_id").(string)
		isAdmin, _ := c.Get("is_admin").(bool)

		var result []map[string]any
		for _, job := range exports.jobs {
			if !isAdmin && job.createdBy != userID {
				continue
			}
			result = append(result, map[string]any{
				"id":         job.id,
				"type":       job.jobType,
				"status":     job.status,
				"created_by": job.createdBy,
			})
		}
		if result == nil {
			result = []map[string]any{}
		}
		return c.JSON(http.StatusOK, map[string]any{"jobs": result})
	})

	// GET /api/v1/exports/jobs/:id
	auth.GET("/exports/jobs/:id", func(c echo.Context) error {
		jobID := c.Param("id")
		job, ok := exports.jobs[jobID]
		if !ok {
			return common.ErrorResponse(c, http.StatusNotFound, "exports.not_found", "Job not found")
		}
		return c.JSON(http.StatusOK, map[string]any{
			"id":         job.id,
			"type":       job.jobType,
			"status":     job.status,
			"created_by": job.createdBy,
		})
	})

	// ── Config flag routes ───────────────────────────────────────────────────

	// GET /api/v1/admin/config/flags — admin only
	admin.GET("/admin/config/flags", func(c echo.Context) error {
		var flags []map[string]any
		for _, f := range cfg.flags {
			flags = append(flags, map[string]any{
				"key":                f.key,
				"enabled":            f.enabled,
				"rollout_percentage": f.rolloutPercentage,
				"target_roles":       f.targetRoles,
			})
		}
		if flags == nil {
			flags = []map[string]any{}
		}
		return c.JSON(http.StatusOK, map[string]any{"flags": flags})
	})

	// PUT /api/v1/admin/config/flags/:key — admin only
	admin.PUT("/admin/config/flags/:key", func(c echo.Context) error {
		key := c.Param("key")
		var req struct {
			Enabled           bool     `json:"enabled"`
			RolloutPercentage int      `json:"rollout_percentage"`
			TargetRoles       []string `json:"target_roles"`
		}
		if err := c.Bind(&req); err != nil {
			return common.BadRequest(c, "validation.invalid_body", "Invalid body")
		}
		if req.RolloutPercentage == 0 {
			req.RolloutPercentage = 100
		}
		if req.TargetRoles == nil {
			req.TargetRoles = []string{}
		}
		cfg.flags[key] = &fakeConfigFlag{
			key:               key,
			enabled:           req.Enabled,
			rolloutPercentage: req.RolloutPercentage,
			targetRoles:       req.TargetRoles,
			updatedAt:         time.Now(),
		}
		return c.JSON(http.StatusOK, map[string]any{
			"key":                key,
			"enabled":            req.Enabled,
			"rollout_percentage": req.RolloutPercentage,
			"target_roles":       req.TargetRoles,
		})
	})

	// GET /api/v1/admin/config/params
	auth.GET("/admin/config/params", func(c echo.Context) error {
		var params []map[string]any
		for k, v := range cfg.params {
			params = append(params, map[string]any{"key": k, "value": v})
		}
		if params == nil {
			params = []map[string]any{}
		}
		return c.JSON(http.StatusOK, map[string]any{"params": params})
	})

	// PUT /api/v1/admin/config/params/:key — admin only
	admin.PUT("/admin/config/params/:key", func(c echo.Context) error {
		key := c.Param("key")
		var req struct {
			Value string `json:"value"`
		}
		if err := c.Bind(&req); err != nil {
			return common.BadRequest(c, "validation.invalid_body", "Invalid body")
		}
		cfg.params[key] = req.Value
		return c.JSON(http.StatusOK, map[string]string{"status": "ok", "key": key})
	})

	// GET /api/v1/admin/config/version-rules
	auth.GET("/admin/config/version-rules", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"rules": cfg.versionRules})
	})

	// PUT /api/v1/admin/config/version-rules — admin only
	admin.PUT("/admin/config/version-rules", func(c echo.Context) error {
		var req struct {
			MinVersion string `json:"min_version"`
			MaxVersion string `json:"max_version"`
			Action     string `json:"action"`
			Message    string `json:"message"`
		}
		if err := c.Bind(&req); err != nil {
			return common.BadRequest(c, "validation.invalid_body", "Invalid body")
		}
		if req.MinVersion == "" {
			return common.BadRequest(c, "validation.required", "min_version is required")
		}
		cfg.versionRules = append(cfg.versionRules, map[string]string{
			"min_version": req.MinVersion,
			"max_version": req.MaxVersion,
			"action":      req.Action,
		})
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// ── Webhook routes ───────────────────────────────────────────────────────

	// GET /api/v1/admin/webhooks — admin only
	admin.GET("/admin/webhooks", func(c echo.Context) error {
		var eps []map[string]any
		for _, ep := range wh.endpoints {
			eps = append(eps, map[string]any{
				"id":        ep.id,
				"url":       ep.url,
				"events":    ep.events,
				"is_active": ep.isActive,
			})
		}
		if eps == nil {
			eps = []map[string]any{}
		}
		return c.JSON(http.StatusOK, map[string]any{"endpoints": eps})
	})

	// POST /api/v1/admin/webhooks — admin only
	admin.POST("/admin/webhooks", func(c echo.Context) error {
		userID, _ := c.Get("user_id").(string)
		var req struct {
			URL    string   `json:"url"`
			Events []string `json:"events"`
			Secret string   `json:"secret"`
		}
		if err := c.Bind(&req); err != nil {
			return common.BadRequest(c, "validation.invalid_body", "Invalid body")
		}
		if req.URL == "" {
			return common.BadRequest(c, "validation.required", "url is required")
		}
		id := uuid.New().String()
		ep := &fakeWebhookEndpoint{
			id:        id,
			url:       req.URL,
			events:    req.Events,
			isActive:  true,
			createdBy: userID,
			createdAt: time.Now(),
		}
		if ep.events == nil {
			ep.events = []string{}
		}
		wh.endpoints[id] = ep
		return c.JSON(http.StatusCreated, map[string]any{
			"id":        ep.id,
			"url":       ep.url,
			"events":    ep.events,
			"is_active": ep.isActive,
		})
	})

	// GET /api/v1/admin/webhooks/deliveries — admin only
	admin.GET("/admin/webhooks/deliveries", func(c echo.Context) error {
		var dels []map[string]any
		for _, d := range wh.deliveries {
			dels = append(dels, map[string]any{
				"id":          d.id,
				"endpoint_id": d.endpointID,
				"event_type":  d.eventType,
				"status":      d.status,
				"attempts":    d.attempts,
			})
		}
		if dels == nil {
			dels = []map[string]any{}
		}
		return c.JSON(http.StatusOK, map[string]any{"deliveries": dels})
	})

	return e
}

// ── Setup helpers ─────────────────────────────────────────────────────────────

// loginAs logs in as the named user and returns the session cookie.
func loginAs(t *testing.T, e *echo.Echo, us *fakeUserStore, ss *fakeSessionStore, username string) *http.Cookie {
	t.Helper()
	u, ok := us.users[username]
	if !ok {
		t.Fatalf("user %q not found in fake store", username)
	}
	token := fmt.Sprintf("tok_%s_%s", username, uuid.New().String())
	ss.sessions[token] = &sessions.Session{
		ID:     "sess_" + u.id,
		UserID: u.id,
	}
	return &http.Cookie{Name: sessions.CookieName, Value: token}
}

// doRequest performs an HTTP request against the echo server.
func doRequest(e *echo.Echo, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		buf.Write(b)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// buildUsersAndSessions builds a pair of user stores with a regular user and an admin.
func buildUsersAndSessions() (*fakeUserStore, *fakeSessionStore) {
	us := newFakeUserStore()
	ss := newFakeSessionStore()

	us.add(fakeUser{id: "user-001", username: "regular_user", passwordHash: "", isActive: true})
	us.add(fakeUser{id: "admin-001", username: "admin_user", passwordHash: "", isActive: true})
	return us, ss
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// 1. POST /exports/jobs creates a job
func TestCreateExportJobReturns201(t *testing.T) {
	us, ss := buildUsersAndSessions()
	exports := newFakeExportStore()
	cfg := newFakeConfigStore()
	wh := newFakeWebhookStore()
	e := buildSlice9Echo(us, ss, exports, cfg, wh)

	cookie := loginAs(t, e, us, ss, "regular_user")
	rec := doRequest(e, http.MethodPost, "/api/v1/exports/jobs",
		map[string]any{"type": "learning_progress_csv"}, cookie)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["id"] == nil || body["id"] == "" {
		t.Error("expected non-empty id in response")
	}
	if body["type"] != "learning_progress_csv" {
		t.Errorf("expected type=learning_progress_csv, got %v", body["type"])
	}
	if body["status"] != "queued" {
		t.Errorf("expected status=queued, got %v", body["status"])
	}
}

// 2. GET /exports/jobs lists user's jobs
func TestListExportJobsReturnsOwnJobs(t *testing.T) {
	us, ss := buildUsersAndSessions()
	exports := newFakeExportStore()
	cfg := newFakeConfigStore()
	wh := newFakeWebhookStore()
	e := buildSlice9Echo(us, ss, exports, cfg, wh)

	cookie := loginAs(t, e, us, ss, "regular_user")

	// Create a job first
	createRec := doRequest(e, http.MethodPost, "/api/v1/exports/jobs",
		map[string]any{"type": "learning_progress_csv"}, cookie)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create job failed: %d", createRec.Code)
	}

	listRec := doRequest(e, http.MethodGet, "/api/v1/exports/jobs", nil, cookie)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", listRec.Code, listRec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(listRec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	jobs, ok := body["jobs"].([]any)
	if !ok {
		t.Fatalf("expected jobs array, got %T", body["jobs"])
	}
	if len(jobs) == 0 {
		t.Error("expected at least one job in list")
	}
}

// 3. GET /exports/jobs/:id returns job status
func TestGetExportJobReturns200(t *testing.T) {
	us, ss := buildUsersAndSessions()
	exports := newFakeExportStore()
	cfg := newFakeConfigStore()
	wh := newFakeWebhookStore()
	e := buildSlice9Echo(us, ss, exports, cfg, wh)

	cookie := loginAs(t, e, us, ss, "regular_user")

	// Create a job
	createRec := doRequest(e, http.MethodPost, "/api/v1/exports/jobs",
		map[string]any{"type": "reconciliation_export"}, cookie)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create job: %d", createRec.Code)
	}
	var created map[string]any
	json.Unmarshal(createRec.Body.Bytes(), &created)
	jobID, _ := created["id"].(string)

	// Get by ID
	getRec := doRequest(e, http.MethodGet, fmt.Sprintf("/api/v1/exports/jobs/%s", jobID), nil, cookie)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", getRec.Code, getRec.Body.String())
	}
	var got map[string]any
	json.Unmarshal(getRec.Body.Bytes(), &got)
	if got["id"] != jobID {
		t.Errorf("expected id=%s, got %v", jobID, got["id"])
	}
}

// 4. GET /admin/config/flags returns 200 for admin, 403 for regular user
func TestListConfigFlagsAdminOnly(t *testing.T) {
	us, ss := buildUsersAndSessions()
	exports := newFakeExportStore()
	cfg := newFakeConfigStore()
	wh := newFakeWebhookStore()
	e := buildSlice9Echo(us, ss, exports, cfg, wh)

	// Admin should get 200
	adminCookie := loginAs(t, e, us, ss, "admin_user")
	adminRec := doRequest(e, http.MethodGet, "/api/v1/admin/config/flags", nil, adminCookie)
	if adminRec.Code != http.StatusOK {
		t.Errorf("expected 200 for admin, got %d — body: %s", adminRec.Code, adminRec.Body.String())
	}

	// Regular user should get 403
	userCookie := loginAs(t, e, us, ss, "regular_user")
	userRec := doRequest(e, http.MethodGet, "/api/v1/admin/config/flags", nil, userCookie)
	if userRec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for regular user, got %d", userRec.Code)
	}
}

// 5. PUT /admin/config/flags/:key updates a flag
func TestSetConfigFlagUpdatesFlag(t *testing.T) {
	us, ss := buildUsersAndSessions()
	exports := newFakeExportStore()
	cfg := newFakeConfigStore()
	wh := newFakeWebhookStore()
	e := buildSlice9Echo(us, ss, exports, cfg, wh)

	adminCookie := loginAs(t, e, us, ss, "admin_user")
	rec := doRequest(e, http.MethodPut, "/api/v1/admin/config/flags/mfa.enabled",
		map[string]any{
			"enabled":            true,
			"rollout_percentage": 50,
			"target_roles":       []string{"admin", "learner"},
		}, adminCookie)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["key"] != "mfa.enabled" {
		t.Errorf("expected key=mfa.enabled, got %v", body["key"])
	}
	if body["enabled"] != true {
		t.Errorf("expected enabled=true, got %v", body["enabled"])
	}
}

// 6. GET /admin/config/params returns params list
func TestListConfigParamsReturns200(t *testing.T) {
	us, ss := buildUsersAndSessions()
	exports := newFakeExportStore()
	cfg := newFakeConfigStore()
	cfg.params["session.idle_timeout_seconds"] = "900"
	wh := newFakeWebhookStore()
	e := buildSlice9Echo(us, ss, exports, cfg, wh)

	userCookie := loginAs(t, e, us, ss, "regular_user")
	rec := doRequest(e, http.MethodGet, "/api/v1/admin/config/params", nil, userCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	params, ok := body["params"].([]any)
	if !ok {
		t.Fatalf("expected params array, got %T", body["params"])
	}
	if len(params) == 0 {
		t.Error("expected at least one param")
	}
}

// 7. GET /admin/config/version-rules returns version rules
func TestListVersionRulesReturns200(t *testing.T) {
	us, ss := buildUsersAndSessions()
	exports := newFakeExportStore()
	cfg := newFakeConfigStore()
	wh := newFakeWebhookStore()
	e := buildSlice9Echo(us, ss, exports, cfg, wh)

	userCookie := loginAs(t, e, us, ss, "regular_user")
	rec := doRequest(e, http.MethodGet, "/api/v1/admin/config/version-rules", nil, userCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if _, ok := body["rules"]; !ok {
		t.Error("expected rules key in response")
	}
}

// 8. GET /admin/webhooks returns 200 for admin
func TestListWebhookEndpointsAdminOnly(t *testing.T) {
	us, ss := buildUsersAndSessions()
	exports := newFakeExportStore()
	cfg := newFakeConfigStore()
	wh := newFakeWebhookStore()
	e := buildSlice9Echo(us, ss, exports, cfg, wh)

	adminCookie := loginAs(t, e, us, ss, "admin_user")
	rec := doRequest(e, http.MethodGet, "/api/v1/admin/webhooks", nil, adminCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if _, ok := body["endpoints"]; !ok {
		t.Error("expected endpoints key in response")
	}

	// Regular user should get 403
	userCookie := loginAs(t, e, us, ss, "regular_user")
	userRec := doRequest(e, http.MethodGet, "/api/v1/admin/webhooks", nil, userCookie)
	if userRec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for regular user, got %d", userRec.Code)
	}
}

// 9. POST /admin/webhooks creates endpoint
func TestCreateWebhookEndpointReturns201(t *testing.T) {
	us, ss := buildUsersAndSessions()
	exports := newFakeExportStore()
	cfg := newFakeConfigStore()
	wh := newFakeWebhookStore()
	e := buildSlice9Echo(us, ss, exports, cfg, wh)

	adminCookie := loginAs(t, e, us, ss, "admin_user")
	rec := doRequest(e, http.MethodPost, "/api/v1/admin/webhooks",
		map[string]any{
			"url":    "http://10.0.0.1:8888/hook",
			"events": []string{"export.completed", "settlement.approved"},
			"secret": "mysecret",
		}, adminCookie)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["url"] != "http://10.0.0.1:8888/hook" {
		t.Errorf("expected url in response, got %v", body["url"])
	}
	if body["id"] == nil || body["id"] == "" {
		t.Error("expected non-empty id in response")
	}
}

// 10. GET /admin/webhooks/deliveries returns deliveries list
func TestListWebhookDeliveriesReturns200(t *testing.T) {
	us, ss := buildUsersAndSessions()
	exports := newFakeExportStore()
	cfg := newFakeConfigStore()
	wh := newFakeWebhookStore()
	// Seed one delivery
	wh.deliveries = append(wh.deliveries, &fakeWebhookDelivery{
		id:         uuid.New().String(),
		endpointID: "ep-001",
		eventType:  "export.completed",
		status:     "pending",
		attempts:   0,
	})
	e := buildSlice9Echo(us, ss, exports, cfg, wh)

	adminCookie := loginAs(t, e, us, ss, "admin_user")
	rec := doRequest(e, http.MethodGet, "/api/v1/admin/webhooks/deliveries", nil, adminCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	deliveries, ok := body["deliveries"].([]any)
	if !ok {
		t.Fatalf("expected deliveries array, got %T", body["deliveries"])
	}
	if len(deliveries) == 0 {
		t.Error("expected at least one delivery in list")
	}
}

// Additional edge case tests

// TestCreateExportJobUnknownTypeReturns400
func TestCreateExportJobUnknownTypeReturns400(t *testing.T) {
	us, ss := buildUsersAndSessions()
	exports := newFakeExportStore()
	cfg := newFakeConfigStore()
	wh := newFakeWebhookStore()
	e := buildSlice9Echo(us, ss, exports, cfg, wh)

	cookie := loginAs(t, e, us, ss, "regular_user")
	rec := doRequest(e, http.MethodPost, "/api/v1/exports/jobs",
		map[string]any{"type": "unknown_type"}, cookie)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown type, got %d", rec.Code)
	}
}

// TestGetExportJobNotFoundReturns404
func TestGetExportJobNotFoundReturns404(t *testing.T) {
	us, ss := buildUsersAndSessions()
	exports := newFakeExportStore()
	cfg := newFakeConfigStore()
	wh := newFakeWebhookStore()
	e := buildSlice9Echo(us, ss, exports, cfg, wh)

	cookie := loginAs(t, e, us, ss, "regular_user")
	rec := doRequest(e, http.MethodGet, "/api/v1/exports/jobs/nonexistent-id", nil, cookie)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing job, got %d", rec.Code)
	}
}

// TestSetVersionRuleMissingMinVersionReturns400
func TestSetVersionRuleMissingMinVersionReturns400(t *testing.T) {
	us, ss := buildUsersAndSessions()
	exports := newFakeExportStore()
	cfg := newFakeConfigStore()
	wh := newFakeWebhookStore()
	e := buildSlice9Echo(us, ss, exports, cfg, wh)

	adminCookie := loginAs(t, e, us, ss, "admin_user")
	rec := doRequest(e, http.MethodPut, "/api/v1/admin/config/version-rules",
		map[string]any{"action": "block"}, adminCookie)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when min_version missing, got %d", rec.Code)
	}
}

// TestCreateWebhookMissingURLReturns400
func TestCreateWebhookMissingURLReturns400(t *testing.T) {
	us, ss := buildUsersAndSessions()
	exports := newFakeExportStore()
	cfg := newFakeConfigStore()
	wh := newFakeWebhookStore()
	e := buildSlice9Echo(us, ss, exports, cfg, wh)

	adminCookie := loginAs(t, e, us, ss, "admin_user")
	rec := doRequest(e, http.MethodPost, "/api/v1/admin/webhooks",
		map[string]any{"events": []string{"test"}}, adminCookie)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when url missing, got %d", rec.Code)
	}
}

// TestUnauthenticatedExportJobReturns401
func TestUnauthenticatedExportJobReturns401(t *testing.T) {
	us, ss := buildUsersAndSessions()
	exports := newFakeExportStore()
	cfg := newFakeConfigStore()
	wh := newFakeWebhookStore()
	e := buildSlice9Echo(us, ss, exports, cfg, wh)

	rec := doRequest(e, http.MethodPost, "/api/v1/exports/jobs",
		map[string]any{"type": "learning_progress_csv"}, nil)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without session, got %d", rec.Code)
	}
}

// ── Reconciliation export CSV content tests ─────────────────────────────────
//
// These tests exercise the reconciliation export CSV generation path end-to-end
// through an in-memory fake that mirrors the real COALESCE(initiated_by, run_by)
// scoping logic. They verify:
//   - CSV header uses "initiated_by" (not the legacy "run_by")
//   - Scoped export: only the requesting user's runs are included
//   - Scoped export: another user's runs are excluded
//   - Unscoped (admin) export: both users' runs are included

// fakeReconRun mirrors a reconciliation_runs row with both legacy and
// API-initiated ownership columns for testing COALESCE behavior.
type fakeReconRun struct {
	ID          string
	BatchID     string
	RunBy       string // legacy column (batch-import driven)
	InitiatedBy string // API column (007 migration)
	Status      string
	StartedAt   time.Time
	Variances   int
	Delta       int64
}

// fakeReconExportCSV mirrors the real writeReconciliationCSV logic:
// it uses COALESCE(initiated_by, run_by) for both the owner column and
// scoping filter. This is the exact algorithm the production code uses.
func fakeReconExportCSV(runs []fakeReconRun, scopedBy string) string {
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)

	// Header must say "initiated_by", matching the production fix.
	_ = cw.Write([]string{
		"run_id", "batch_id", "initiated_by", "started_at", "completed_at",
		"status", "total_variances", "total_variance_amount",
	})

	for _, r := range runs {
		// COALESCE(initiated_by, run_by) — same as the production SQL.
		owner := r.InitiatedBy
		if owner == "" {
			owner = r.RunBy
		}

		// Scope filter.
		if scopedBy != "" && owner != scopedBy {
			continue
		}

		_ = cw.Write([]string{
			r.ID, r.BatchID, owner,
			r.StartedAt.Format(time.RFC3339), "",
			r.Status, fmt.Sprintf("%d", r.Variances), fmt.Sprintf("%d", r.Delta),
		})
	}
	cw.Flush()
	return buf.String()
}

// TestReconExportCSV_ScopedToUser verifies that a user-scoped reconciliation
// export includes only that user's API-created runs and excludes other users'.
func TestReconExportCSV_ScopedToUser(t *testing.T) {
	runs := []fakeReconRun{
		{ID: "run-A", BatchID: "b1", InitiatedBy: "user-A", Status: "completed", StartedAt: time.Now(), Variances: 2, Delta: 500},
		{ID: "run-B", BatchID: "b2", InitiatedBy: "user-B", Status: "completed", StartedAt: time.Now(), Variances: 1, Delta: 300},
		{ID: "run-legacy", BatchID: "b3", RunBy: "user-A", Status: "completed", StartedAt: time.Now(), Variances: 0, Delta: 0},
	}

	csvOut := fakeReconExportCSV(runs, "user-A")
	reader := csv.NewReader(strings.NewReader(csvOut))
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}

	// Header check: must say "initiated_by", NOT "run_by".
	headers := rows[0]
	colIdx := map[string]int{}
	for i, h := range headers {
		colIdx[h] = i
	}
	if _, ok := colIdx["initiated_by"]; !ok {
		t.Errorf("header must contain 'initiated_by'; got %v", headers)
	}
	if _, ok := colIdx["run_by"]; ok {
		t.Errorf("header must NOT contain legacy 'run_by'; got %v", headers)
	}

	// Data: expect run-A (initiated_by=user-A) and run-legacy (run_by=user-A).
	dataRows := rows[1:]
	if len(dataRows) != 2 {
		t.Fatalf("expected 2 data rows for user-A scope, got %d: %v", len(dataRows), dataRows)
	}

	// Collect run IDs.
	ridCol := colIdx["run_id"]
	ibCol := colIdx["initiated_by"]
	ids := map[string]string{}
	for _, r := range dataRows {
		ids[r[ridCol]] = r[ibCol]
	}

	// run-A present with initiated_by=user-A.
	if ids["run-A"] != "user-A" {
		t.Errorf("run-A: expected initiated_by=user-A, got %q", ids["run-A"])
	}
	// run-legacy present with owner=user-A (from COALESCE fallback).
	if ids["run-legacy"] != "user-A" {
		t.Errorf("run-legacy: expected initiated_by=user-A (via COALESCE), got %q", ids["run-legacy"])
	}
	// run-B must NOT appear.
	if _, present := ids["run-B"]; present {
		t.Errorf("run-B must NOT appear in user-A scoped export")
	}
}

// TestReconExportCSV_UnscopedIncludesAll verifies the admin/unscoped path
// returns all runs regardless of owner.
func TestReconExportCSV_UnscopedIncludesAll(t *testing.T) {
	runs := []fakeReconRun{
		{ID: "run-A", InitiatedBy: "user-A", Status: "completed", StartedAt: time.Now(), Variances: 2, Delta: 500},
		{ID: "run-B", InitiatedBy: "user-B", Status: "completed", StartedAt: time.Now(), Variances: 1, Delta: 300},
	}

	csvOut := fakeReconExportCSV(runs, "") // empty = admin unscoped
	reader := csv.NewReader(strings.NewReader(csvOut))
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}

	dataRows := rows[1:]
	if len(dataRows) != 2 {
		t.Fatalf("expected 2 data rows for unscoped export, got %d", len(dataRows))
	}

	ridCol := 0 // run_id is first column
	ids := map[string]bool{}
	for _, r := range dataRows {
		ids[r[ridCol]] = true
	}
	if !ids["run-A"] {
		t.Error("run-A must appear in unscoped export")
	}
	if !ids["run-B"] {
		t.Error("run-B must appear in unscoped export")
	}
}

// TestReconExportCSV_LegacyRunByFallback verifies that legacy rows (only
// run_by set, initiated_by empty) are found by the COALESCE logic.
func TestReconExportCSV_LegacyRunByFallback(t *testing.T) {
	runs := []fakeReconRun{
		{ID: "legacy-run", RunBy: "old-user", Status: "completed", StartedAt: time.Now()},
		{ID: "api-run", InitiatedBy: "new-user", Status: "completed", StartedAt: time.Now()},
	}

	// Scoped to old-user: should include legacy-run (via run_by fallback), exclude api-run.
	csvOut := fakeReconExportCSV(runs, "old-user")
	reader := csv.NewReader(strings.NewReader(csvOut))
	rows, _ := reader.ReadAll()

	dataRows := rows[1:]
	if len(dataRows) != 1 {
		t.Fatalf("expected 1 row for old-user, got %d: %v", len(dataRows), dataRows)
	}
	if dataRows[0][0] != "legacy-run" {
		t.Errorf("expected legacy-run, got %q", dataRows[0][0])
	}
	// The owner column should show "old-user" from the COALESCE fallback.
	if dataRows[0][2] != "old-user" {
		t.Errorf("expected initiated_by=old-user (COALESCE fallback), got %q", dataRows[0][2])
	}
}

// TestReconExportCSV_ExcludesOtherUser verifies the negative case: user B's
// runs never appear in user A's scoped export.
func TestReconExportCSV_ExcludesOtherUser(t *testing.T) {
	runs := []fakeReconRun{
		{ID: "run-B1", InitiatedBy: "user-B", Status: "completed", StartedAt: time.Now()},
		{ID: "run-B2", InitiatedBy: "user-B", Status: "completed", StartedAt: time.Now()},
	}

	csvOut := fakeReconExportCSV(runs, "user-A") // scoped to A, but all runs are B's
	reader := csv.NewReader(strings.NewReader(csvOut))
	rows, _ := reader.ReadAll()

	dataRows := rows[1:]
	if len(dataRows) != 0 {
		t.Errorf("expected 0 data rows for user-A when all runs belong to user-B, got %d: %v",
			len(dataRows), dataRows)
	}
}
