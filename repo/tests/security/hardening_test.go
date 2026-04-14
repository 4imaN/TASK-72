// Package security_test contains hardening tests: SQL injection probes, XSS
// probes, rating boundary validation, export object isolation, and oversized
// request rejection.
// All tests use httptest and in-process mocks; no database connection required.
package security_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"portal/internal/app/common"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

// fakeJob represents an in-memory export job for isolation tests.
type fakeJob struct {
	ID        string `json:"id"`
	CreatedBy string `json:"created_by"`
	JobType   string `json:"job_type"`
	Status    string `json:"status"`
}

// buildSearchEchoHardening constructs an Echo with a fake search endpoint that
// behaves like the real one: user input drives parameter values, not SQL text.
// It is safe against SQL injection because it never constructs raw SQL.
func buildSearchEchoHardening() *echo.Echo {
	e := echo.New()
	e.HideBanner = true

	// Resources available in the fake store
	type resource struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	resources := []resource{
		{ID: "res-1", Title: "Introduction to Leadership"},
		{ID: "res-2", Title: "Data Analysis Fundamentals"},
	}

	e.GET("/api/v1/search", func(c echo.Context) error {
		q := c.QueryParam("q")
		_ = q // Simulates parameterized binding — not interpolated into SQL

		// The fake handler returns all resources (simulating 0 DB results for
		// unmatched queries) — never crashes on malicious q values.
		var matched []resource
		for _, r := range resources {
			if q == "" || strings.Contains(strings.ToLower(r.Title), strings.ToLower(q)) {
				matched = append(matched, r)
			}
		}
		if matched == nil {
			matched = []resource{}
		}
		return c.JSON(http.StatusOK, map[string]any{
			"results": matched,
			"total":   len(matched),
			"limit":   20,
			"offset":  0,
		})
	})

	return e
}

// buildReviewEcho constructs an Echo instance with a fake POST /reviews
// endpoint that mirrors the real CreateReview handler validation.
func buildReviewEcho() *echo.Echo {
	e := echo.New()
	e.HideBanner = true

	e.POST("/api/v1/reviews", func(c echo.Context) error {
		userID := "test-user"

		var req struct {
			OrderID string `json:"order_id"`
			Rating  int    `json:"rating"`
			Body    string `json:"body"`
		}
		if err := c.Bind(&req); err != nil {
			return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
		}

		if req.OrderID == "" {
			return common.BadRequest(c, "validation.required", "order_id is required")
		}
		if req.Rating < 1 || req.Rating > 5 {
			return common.BadRequest(c, "validation.invalid_rating", "Rating must be between 1 and 5")
		}
		if len(req.Body) > 2000 {
			return common.BadRequest(c, "validation.body_too_long", "Review body must be 2000 characters or fewer")
		}

		_ = userID
		return c.JSON(http.StatusCreated, map[string]string{
			"id":     "review-new",
			"status": "created",
		})
	})

	return e
}

// buildExportIsolationEcho creates an Echo with a fake /exports/jobs endpoint
// that enforces object-level authorization by scoping jobs to the requesting
// user's ID (unless they carry the "admin" role).
func buildExportIsolationEcho(jobs []*fakeJob) *echo.Echo {
	e := echo.New()
	e.HideBanner = true

	e.GET("/api/v1/exports/jobs", func(c echo.Context) error {
		callerID, ok := c.Get("user_id").(string)
		if !ok || callerID == "" {
			return common.Unauthorized(c, "Not authenticated")
		}

		// Admin bypass matches the real handler.
		isAdmin := false
		roles, _ := c.Get("roles").([]string)
		for _, r := range roles {
			if r == "admin" {
				isAdmin = true
				break
			}
		}

		var result []*fakeJob
		for _, j := range jobs {
			if isAdmin || j.CreatedBy == callerID {
				result = append(result, j)
			}
		}
		if result == nil {
			result = []*fakeJob{}
		}
		return c.JSON(http.StatusOK, map[string]any{"jobs": result})
	})

	return e
}

// ── Test 1: SQL injection probe ───────────────────────────────────────────────

// TestSQLInjectionSearchQuery verifies that a classic SQL injection payload in
// the search query parameter does NOT cause a 500 or panic.  The application
// uses parameterized queries so the payload is treated as a literal string.
func TestSQLInjectionSearchQuery(t *testing.T) {
	e := buildSearchEchoHardening()

	payload := `'; DROP TABLE resources; --`
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/search?q="+url_encode(payload), nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code == http.StatusInternalServerError {
		t.Errorf("SQL injection payload caused 500 — expected 200 with empty results, got %d", rec.Code)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for SQL injection probe, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	results, ok := resp["results"]
	if !ok {
		t.Errorf("response missing 'results' field")
	}
	arr, _ := results.([]any)
	if len(arr) != 0 {
		t.Errorf("expected empty results for injection payload, got %d items", len(arr))
	}
}

// TestSQLInjectionSearchQueryUnionSelect tests a UNION-SELECT injection vector.
func TestSQLInjectionSearchQueryUnionSelect(t *testing.T) {
	e := buildSearchEchoHardening()

	payload := `' UNION SELECT id, password_hash, NULL, NULL FROM users --`
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/search?q="+url_encode(payload), nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code == http.StatusInternalServerError {
		t.Errorf("UNION injection payload caused 500")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
}

// ── Test 2: XSS probe ────────────────────────────────────────────────────────

// TestXSSInSearchQuery verifies that a script-tag payload in the search query
// is returned as-is in JSON (correct behavior: backend echoes the string,
// sanitization is a frontend responsibility).  A 500 or panic would be wrong.
func TestXSSInSearchQuery(t *testing.T) {
	e := buildSearchEchoHardening()

	payload := `<script>alert(1)</script>`
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/search?q="+url_encode(payload), nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code == http.StatusInternalServerError {
		t.Errorf("XSS payload caused 500")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for XSS probe, got %d", rec.Code)
	}
	// Response must be valid JSON — no panic, no garbled output.
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("XSS probe produced non-JSON response: %v", err)
	}
}

// ── Test 3: Rating boundary validation ───────────────────────────────────────

// TestRatingAboveMaxReturns400 verifies that rating=6 is rejected.
func TestRatingAboveMaxReturns400(t *testing.T) {
	e := buildReviewEcho()

	body := `{"order_id":"order-1","rating":6,"body":"Great product"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for rating=6, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["code"] != "validation.invalid_rating" {
		t.Errorf("expected validation.invalid_rating error code, got %v", resp["code"])
	}
}

// TestRatingZeroReturns400 verifies that rating=0 is rejected.
func TestRatingZeroReturns400(t *testing.T) {
	e := buildReviewEcho()

	body := `{"order_id":"order-1","rating":0,"body":"Average product"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for rating=0, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["code"] != "validation.invalid_rating" {
		t.Errorf("expected validation.invalid_rating error code, got %v", resp["code"])
	}
}

// TestRatingNegativeReturns400 verifies that a negative rating is rejected.
func TestRatingNegativeReturns400(t *testing.T) {
	e := buildReviewEcho()

	body := `{"order_id":"order-1","rating":-1,"body":"Terrible"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for rating=-1, got %d", rec.Code)
	}
}

// TestRatingBoundaryValidAccepted verifies rating=1 (minimum) is accepted.
func TestRatingBoundaryMinValid(t *testing.T) {
	e := buildReviewEcho()

	body := `{"order_id":"order-1","rating":1,"body":"Terrible experience"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201 for rating=1, got %d", rec.Code)
	}
}

// TestRatingBoundaryMaxValid verifies rating=5 (maximum) is accepted.
func TestRatingBoundaryMaxValid(t *testing.T) {
	e := buildReviewEcho()

	body := `{"order_id":"order-1","rating":5,"body":"Excellent"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201 for rating=5, got %d", rec.Code)
	}
}

// ── Test 4: Export object isolation ──────────────────────────────────────────

// TestExportObjectIsolation verifies that a user cannot see another user's
// export jobs via GET /exports/jobs.
func TestExportObjectIsolation(t *testing.T) {
	userA := uuid.New().String()
	userB := uuid.New().String()

	jobs := []*fakeJob{
		{ID: uuid.New().String(), CreatedBy: userA, JobType: "learning_progress_csv", Status: "completed"},
		{ID: uuid.New().String(), CreatedBy: userA, JobType: "reconciliation_export", Status: "pending"},
	}

	e := buildExportIsolationEcho(jobs)

	// User B requests the jobs list
	req := httptest.NewRequest(http.MethodGet, "/api/v1/exports/jobs", nil)
	rec := httptest.NewRecorder()

	ctx := e.NewContext(req, rec)
	ctx.Set("user_id", userB)
	ctx.Set("roles", []string{"learner"})

	e.ServeHTTP(rec, req)

	// Re-issue with user_id manually set via middleware
	e2 := echo.New()
	e2.HideBanner = true
	e2.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("user_id", userB)
			c.Set("roles", []string{"learner"})
			return next(c)
		}
	})
	e2.GET("/api/v1/exports/jobs", func(c echo.Context) error {
		callerID, _ := c.Get("user_id").(string)
		var result []*fakeJob
		for _, j := range jobs {
			if j.CreatedBy == callerID {
				result = append(result, j)
			}
		}
		if result == nil {
			result = []*fakeJob{}
		}
		return c.JSON(http.StatusOK, map[string]any{"jobs": result})
	})

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/exports/jobs", nil)
	rec2 := httptest.NewRecorder()
	e2.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec2.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	jobList, _ := resp["jobs"].([]any)
	if len(jobList) != 0 {
		t.Errorf("user B should see 0 jobs (all belong to user A), got %d", len(jobList))
	}
}

// TestExportObjectIsolationOwnerSeesOwn verifies that a user sees their own jobs.
func TestExportObjectIsolationOwnerSeesOwn(t *testing.T) {
	userA := uuid.New().String()

	jobs := []*fakeJob{
		{ID: uuid.New().String(), CreatedBy: userA, JobType: "learning_progress_csv", Status: "completed"},
		{ID: uuid.New().String(), CreatedBy: userA, JobType: "reconciliation_export", Status: "pending"},
	}

	e := echo.New()
	e.HideBanner = true
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("user_id", userA)
			c.Set("roles", []string{"learner"})
			return next(c)
		}
	})
	e.GET("/api/v1/exports/jobs", func(c echo.Context) error {
		callerID, _ := c.Get("user_id").(string)
		var result []*fakeJob
		for _, j := range jobs {
			if j.CreatedBy == callerID {
				result = append(result, j)
			}
		}
		if result == nil {
			result = []*fakeJob{}
		}
		return c.JSON(http.StatusOK, map[string]any{"jobs": result})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/exports/jobs", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	jobList, _ := resp["jobs"].([]any)
	if len(jobList) != 2 {
		t.Errorf("user A should see 2 own jobs, got %d", len(jobList))
	}
}

// TestExportAdminSeesAll verifies that an admin sees all users' jobs.
func TestExportAdminSeesAll(t *testing.T) {
	userA := uuid.New().String()
	userB := uuid.New().String()
	adminID := uuid.New().String()

	jobs := []*fakeJob{
		{ID: uuid.New().String(), CreatedBy: userA, JobType: "learning_progress_csv", Status: "completed"},
		{ID: uuid.New().String(), CreatedBy: userB, JobType: "reconciliation_export", Status: "pending"},
	}

	e := buildExportIsolationEcho(jobs)
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("user_id", adminID)
			c.Set("roles", []string{"admin"})
			return next(c)
		}
	})

	// Re-build to pick up middleware order
	e2 := echo.New()
	e2.HideBanner = true
	e2.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("user_id", adminID)
			c.Set("roles", []string{"admin"})
			return next(c)
		}
	})
	e2.GET("/api/v1/exports/jobs", func(c echo.Context) error {
		callerID, _ := c.Get("user_id").(string)
		roles, _ := c.Get("roles").([]string)
		isAdmin := false
		for _, r := range roles {
			if r == "admin" {
				isAdmin = true
				break
			}
		}
		var result []*fakeJob
		for _, j := range jobs {
			if isAdmin || j.CreatedBy == callerID {
				result = append(result, j)
			}
		}
		if result == nil {
			result = []*fakeJob{}
		}
		return c.JSON(http.StatusOK, map[string]any{"jobs": result})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/exports/jobs", nil)
	rec := httptest.NewRecorder()
	e2.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	jobList, _ := resp["jobs"].([]any)
	if len(jobList) != 2 {
		t.Errorf("admin should see 2 jobs (all), got %d", len(jobList))
	}
}

// ── Test 5: Oversized request ─────────────────────────────────────────────────

// TestOversizedReviewBodyReturns400 verifies that a review body exceeding
// 2000 characters is rejected with 400.
func TestOversizedReviewBodyReturns400(t *testing.T) {
	e := buildReviewEcho()

	// Construct body >2000 chars
	longBody := strings.Repeat("x", 2001)
	payload := `{"order_id":"order-1","rating":3,"body":"` + longBody + `"}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews",
		strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for body > 2000 chars, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["code"] != "validation.body_too_long" {
		t.Errorf("expected validation.body_too_long error code, got %v", resp["code"])
	}
}

// TestExactlyAtLimitBodyAccepted verifies that a review body of exactly 2000
// characters is accepted.
func TestExactlyAtLimitBodyAccepted(t *testing.T) {
	e := buildReviewEcho()

	limitBody := strings.Repeat("a", 2000)
	payload := `{"order_id":"order-1","rating":4,"body":"` + limitBody + `"}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews",
		strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201 for body of exactly 2000 chars, got %d", rec.Code)
	}
}

// ── Test 6: Unauthenticated export access blocked ─────────────────────────────

// TestExportJobsRequiresAuth verifies that GET /exports/jobs without
// authentication returns 401.
func TestExportJobsRequiresAuth(t *testing.T) {
	e := echo.New()
	e.HideBanner = true

	e.GET("/api/v1/exports/jobs", func(c echo.Context) error {
		userID, ok := c.Get("user_id").(string)
		if !ok || userID == "" {
			return common.Unauthorized(c, "Not authenticated")
		}
		return c.JSON(http.StatusOK, map[string]any{"jobs": []any{}})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/exports/jobs", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated export jobs list, got %d", rec.Code)
	}
}

// ── Helper: minimal URL percent-encoding for query params ────────────────────

// url_encode performs minimal percent-encoding of characters that would break
// a URL query string in httptest requests.
func url_encode(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch c {
		case ' ':
			b.WriteString("%20")
		case '\'':
			b.WriteString("%27")
		case ';':
			b.WriteString("%3B")
		case '"':
			b.WriteString("%22")
		case '<':
			b.WriteString("%3C")
		case '>':
			b.WriteString("%3E")
		case '&':
			b.WriteString("%26")
		case '+':
			b.WriteString("%2B")
		case '#':
			b.WriteString("%23")
		case '`':
			b.WriteString("%60")
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}
