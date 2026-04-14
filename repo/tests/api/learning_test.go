// tests/api/learning_test.go — HTTP-level tests for learning endpoints.
// All tests use httptest and in-process mocks; no real database is required.
package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"portal/internal/app/common"
	"portal/internal/app/sessions"
)

// ── In-memory learning domain ────────────────────────────────────────────────

type fakeLearningPath struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	IsPublished bool     `json:"is_published"`
	ResourceIDs []string `json:"-"` // test-only: resource IDs in this path for enrollment validation
	Rules       *struct {
		RequiredCount   int    `json:"required_count"`
		ElectiveMinimum int    `json:"elective_minimum"`
		Description     string `json:"completion_description,omitempty"`
	} `json:"rules,omitempty"`
}

type fakeEnrollment struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	PathID     string     `json:"path_id"`
	EnrolledAt time.Time  `json:"enrolled_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Status     string     `json:"status"`
}

type fakeProgressSnapshot struct {
	UserID      string     `json:"user_id"`
	ResourceID  string     `json:"resource_id"`
	Status      string     `json:"status"`
	ProgressPct float64    `json:"progress_pct"`
	LastPos     int        `json:"last_position_seconds"`
	LastActive  *time.Time `json:"last_active_at,omitempty"`
}

type fakeLearningStore struct {
	paths       map[string]*fakeLearningPath
	enrollments map[string]*fakeEnrollment // key: userID+":"+pathID
	snapshots   map[string]*fakeProgressSnapshot // key: userID+":"+resourceID
}

func newFakeLearningStore() *fakeLearningStore {
	return &fakeLearningStore{
		paths:       make(map[string]*fakeLearningPath),
		enrollments: make(map[string]*fakeEnrollment),
		snapshots:   make(map[string]*fakeProgressSnapshot),
	}
}

func (s *fakeLearningStore) enrollKey(userID, pathID string) string {
	return userID + ":" + pathID
}

func (s *fakeLearningStore) snapKey(userID, resourceID string) string {
	return userID + ":" + resourceID
}

// buildLearningEcho constructs an Echo instance with learning routes using
// in-memory fake stores. A fake session map is re-used from auth tests.
func buildLearningEcho(
	ls *fakeLearningStore,
	ss *fakeSessionStore,
) *echo.Echo {
	e := echo.New()
	e.HideBanner = true

	// Inline requireAuth middleware (same pattern as auth_test.go)
	requireAuth := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			token := sessions.TokenFromRequest(c)
			if token == "" {
				return common.Unauthorized(c, "Authentication required")
			}
			sess, ok := ss.sessions[token]
			if !ok {
				sessions.ClearCookie(c)
				return common.Unauthorized(c, "Session expired or invalid")
			}
			c.Set("user_id", sess.UserID)
			return next(c)
		}
	}

	// GET /api/v1/paths
	e.GET("/api/v1/paths", requireAuth(func(c echo.Context) error {
		var out []fakeLearningPath
		for _, p := range ls.paths {
			if p.IsPublished {
				out = append(out, *p)
			}
		}
		if out == nil {
			out = []fakeLearningPath{}
		}
		return c.JSON(http.StatusOK, map[string]any{"paths": out})
	}))

	// GET /api/v1/paths/:id
	e.GET("/api/v1/paths/:id", requireAuth(func(c echo.Context) error {
		pathID := c.Param("id")
		p, ok := ls.paths[pathID]
		if !ok {
			return common.ErrorResponse(c, http.StatusNotFound, "learning.not_found", "Path not found")
		}
		return c.JSON(http.StatusOK, p)
	}))

	// POST /api/v1/paths/:id/enroll
	e.POST("/api/v1/paths/:id/enroll", requireAuth(func(c echo.Context) error {
		userID := c.Get("user_id").(string)
		pathID := c.Param("id")

		key := ls.enrollKey(userID, pathID)
		if existing, ok := ls.enrollments[key]; ok {
			return c.JSON(http.StatusOK, existing) // idempotent
		}

		if _, ok := ls.paths[pathID]; !ok {
			return common.ErrorResponse(c, http.StatusNotFound, "learning.not_found", "Path not found")
		}

		e := &fakeEnrollment{
			ID:         "enroll-" + userID + "-" + pathID,
			UserID:     userID,
			PathID:     pathID,
			EnrolledAt: time.Now(),
			Status:     "active",
		}
		ls.enrollments[key] = e
		return c.JSON(http.StatusOK, e)
	}))

	// GET /api/v1/paths/:id/progress
	e.GET("/api/v1/paths/:id/progress", requireAuth(func(c echo.Context) error {
		userID := c.Get("user_id").(string)
		pathID := c.Param("id")

		key := ls.enrollKey(userID, pathID)
		enroll, ok := ls.enrollments[key]
		if !ok {
			return common.ErrorResponse(c, http.StatusNotFound, "learning.not_enrolled", "Not enrolled in this path")
		}
		p, _ := ls.paths[pathID]

		return c.JSON(http.StatusOK, map[string]any{
			"path":             p,
			"enrollment":       enroll,
			"required_items":   []any{},
			"elective_items":   []any{},
			"completion_ready": false,
			"required_done":    0,
			"elective_done":    0,
		})
	}))

	// GET /api/v1/me/progress
	e.GET("/api/v1/me/progress", requireAuth(func(c echo.Context) error {
		userID := c.Get("user_id").(string)
		var inProgress []map[string]any
		for _, snap := range ls.snapshots {
			if snap.UserID == userID && snap.Status == "in_progress" {
				inProgress = append(inProgress, map[string]any{
					"resource_id":           snap.ResourceID,
					"status":                snap.Status,
					"progress_pct":          snap.ProgressPct,
					"last_position_seconds": snap.LastPos,
					"last_active_at":        snap.LastActive,
				})
			}
		}
		if inProgress == nil {
			inProgress = []map[string]any{}
		}
		return c.JSON(http.StatusOK, map[string]any{"in_progress": inProgress})
	}))

	// POST /api/v1/me/progress/:resource_id
	e.POST("/api/v1/me/progress/:resource_id", requireAuth(func(c echo.Context) error {
		userID := c.Get("user_id").(string)
		resourceID := c.Param("resource_id")

		var req struct {
			EventType   string  `json:"event_type"`
			PositionSec int     `json:"position_seconds"`
			ProgressPct float64 `json:"progress_pct"`
		}
		if err := c.Bind(&req); err != nil {
			return common.BadRequest(c, "validation.invalid_body", "Invalid body")
		}
		if req.EventType == "" {
			req.EventType = "progress"
		}

		// Validate that the resource belongs to at least one enrolled path.
		enrolled := false
		for key, enroll := range ls.enrollments {
			if !strings.HasPrefix(key, userID+":") || enroll.Status != "active" {
				continue
			}
			path, ok := ls.paths[enroll.PathID]
			if !ok {
				continue
			}
			if path.ResourceIDs != nil {
				for _, rid := range path.ResourceIDs {
					if rid == resourceID {
						enrolled = true
						break
					}
				}
			}
			if enrolled {
				break
			}
		}
		if !enrolled {
			return common.ErrorResponse(c, http.StatusUnprocessableEntity,
				"learning.resource_not_enrolled",
				"Resource does not belong to any learning path you are enrolled in")
		}

		status := "in_progress"
		if req.EventType == "completed" || req.ProgressPct >= 100 {
			status = "completed"
		}

		now := time.Now()
		key := ls.snapKey(userID, resourceID)
		ls.snapshots[key] = &fakeProgressSnapshot{
			UserID:      userID,
			ResourceID:  resourceID,
			Status:      status,
			ProgressPct: req.ProgressPct,
			LastPos:     req.PositionSec,
			LastActive:  &now,
		}

		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}))

	// GET /api/v1/me/exports/csv
	e.GET("/api/v1/me/exports/csv", requireAuth(func(c echo.Context) error {
		userID := c.Get("user_id").(string)

		c.Response().Header().Set("Content-Type", "text/csv; charset=utf-8")
		c.Response().Header().Set("Content-Disposition", `attachment; filename="learning-record.csv"`)

		// Write a minimal CSV containing only this user's data
		var sb strings.Builder
		sb.WriteString("path_id,path_title,enrollment_status,enrolled_at,completed_at,resource_id,resource_title,content_type,item_type,progress_status,progress_pct,resource_completed_at\n")

		for key, enroll := range ls.enrollments {
			if !strings.HasPrefix(key, userID+":") {
				continue
			}
			sb.WriteString(enroll.PathID + "," + "Test Path" + "," + enroll.Status + "," + enroll.EnrolledAt.Format(time.RFC3339) + ",,,,,,not_started,0,\n")
		}

		_, err := c.Response().Writer.Write([]byte(sb.String()))
		if err != nil {
			return nil
		}
		return nil
	}))

	return e
}

// ── Test helpers ──────────────────────────────────────────────────────────────

func makeLearningSession(ss *fakeSessionStore, userID string) *http.Cookie {
	token := "tok_" + userID
	ss.sessions[token] = &sessions.Session{
		ID:     "sess_" + userID,
		UserID: userID,
	}
	return &http.Cookie{
		Name:     sessions.CookieName,
		Value:    token,
		HttpOnly: true,
	}
}

func doLearningGet(e *echo.Echo, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func doLearningPost(e *echo.Echo, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	var buf *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(http.MethodPost, path, buf)
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// 1. TestListPathsRequiresAuth — without cookie returns 401
func TestListPathsRequiresAuth(t *testing.T) {
	ls := newFakeLearningStore()
	ss := newFakeSessionStore()
	e := buildLearningEcho(ls, ss)

	rec := doLearningGet(e, "/api/v1/paths", nil) // no cookie

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// 2. TestListPathsReturnsPublishedPaths — with auth returns list
func TestListPathsReturnsPublishedPaths(t *testing.T) {
	ls := newFakeLearningStore()
	ss := newFakeSessionStore()

	ls.paths["path-1"] = &fakeLearningPath{ID: "path-1", Title: "Leadership Essentials", IsPublished: true}
	ls.paths["path-2"] = &fakeLearningPath{ID: "path-2", Title: "Data Fundamentals", IsPublished: true}
	ls.paths["path-draft"] = &fakeLearningPath{ID: "path-draft", Title: "Draft Path", IsPublished: false}

	e := buildLearningEcho(ls, ss)
	cookie := makeLearningSession(ss, "user-a")

	rec := doLearningGet(e, "/api/v1/paths", cookie)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	paths, ok := body["paths"].([]any)
	if !ok {
		t.Fatal("expected paths array")
	}
	// Only 2 published paths
	if len(paths) != 2 {
		t.Errorf("expected 2 published paths, got %d", len(paths))
	}
}

// 3. TestEnrollInPath — enroll returns enrollment object
func TestEnrollInPath(t *testing.T) {
	ls := newFakeLearningStore()
	ss := newFakeSessionStore()

	ls.paths["path-1"] = &fakeLearningPath{ID: "path-1", Title: "Leadership Essentials", IsPublished: true}

	e := buildLearningEcho(ls, ss)
	cookie := makeLearningSession(ss, "user-b")

	rec := doLearningPost(e, "/api/v1/paths/path-1/enroll", nil, cookie)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	var enrollment map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &enrollment); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if enrollment["path_id"] != "path-1" {
		t.Errorf("expected path_id=path-1, got %v", enrollment["path_id"])
	}
	if enrollment["status"] != "active" {
		t.Errorf("expected status=active, got %v", enrollment["status"])
	}
	if enrollment["user_id"] != "user-b" {
		t.Errorf("expected user_id=user-b, got %v", enrollment["user_id"])
	}
}

// 4. TestEnrollIdempotent — enroll twice returns same enrollment
func TestEnrollIdempotent(t *testing.T) {
	ls := newFakeLearningStore()
	ss := newFakeSessionStore()

	ls.paths["path-1"] = &fakeLearningPath{ID: "path-1", Title: "Leadership", IsPublished: true}

	e := buildLearningEcho(ls, ss)
	cookie := makeLearningSession(ss, "user-c")

	// First enrollment
	rec1 := doLearningPost(e, "/api/v1/paths/path-1/enroll", nil, cookie)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first enroll: expected 200, got %d", rec1.Code)
	}

	var e1 map[string]any
	_ = json.Unmarshal(rec1.Body.Bytes(), &e1)

	// Second enrollment (idempotent)
	rec2 := doLearningPost(e, "/api/v1/paths/path-1/enroll", nil, cookie)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second enroll: expected 200, got %d", rec2.Code)
	}

	var e2 map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &e2)

	// Same enrollment ID returned
	if e1["id"] != e2["id"] {
		t.Errorf("expected same enrollment id on idempotent enroll: %v != %v", e1["id"], e2["id"])
	}
}

// 5. TestGetPathProgressNotEnrolled — 404 if not enrolled
func TestGetPathProgressNotEnrolled(t *testing.T) {
	ls := newFakeLearningStore()
	ss := newFakeSessionStore()

	ls.paths["path-1"] = &fakeLearningPath{ID: "path-1", Title: "Leadership", IsPublished: true}

	e := buildLearningEcho(ls, ss)
	cookie := makeLearningSession(ss, "user-d")

	rec := doLearningGet(e, "/api/v1/paths/path-1/progress", cookie)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 when not enrolled, got %d", rec.Code)
	}

	var errBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if errBody["code"] != "learning.not_enrolled" {
		t.Errorf("expected code=learning.not_enrolled, got %v", errBody["code"])
	}
}

// 6. TestRecordProgressUpdatesSnapshot — POST progress, GET resume shows it
func TestRecordProgressUpdatesSnapshot(t *testing.T) {
	ls := newFakeLearningStore()
	ss := newFakeSessionStore()

	// Set up path containing the resource, and enroll the user.
	ls.paths["path-e"] = &fakeLearningPath{
		ID: "path-e", Title: "Test Path", IsPublished: true,
		ResourceIDs: []string{"resource-xyz"},
	}

	e := buildLearningEcho(ls, ss)
	cookie := makeLearningSession(ss, "user-e")

	// Enroll user-e in the path first.
	doLearningPost(e, "/api/v1/paths/path-e/enroll", nil, cookie)

	// POST progress
	body := map[string]any{
		"event_type":       "progress",
		"position_seconds": 120,
		"progress_pct":     45.0,
	}
	postRec := doLearningPost(e, "/api/v1/me/progress/resource-xyz", body, cookie)
	if postRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", postRec.Code, postRec.Body.String())
	}

	// GET resume state — should show the resource as in_progress
	getRec := doLearningGet(e, "/api/v1/me/progress", cookie)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", getRec.Code)
	}

	var resumeBody map[string]any
	if err := json.Unmarshal(getRec.Body.Bytes(), &resumeBody); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	inProgress, ok := resumeBody["in_progress"].([]any)
	if !ok {
		t.Fatal("expected in_progress array")
	}
	if len(inProgress) != 1 {
		t.Fatalf("expected 1 in-progress item, got %d", len(inProgress))
	}

	item := inProgress[0].(map[string]any)
	if item["resource_id"] != "resource-xyz" {
		t.Errorf("expected resource_id=resource-xyz, got %v", item["resource_id"])
	}
	if item["progress_pct"].(float64) != 45.0 {
		t.Errorf("expected progress_pct=45.0, got %v", item["progress_pct"])
	}
}

// 7. TestCSVExportRequiresAuth — without cookie returns 401
func TestCSVExportRequiresAuth(t *testing.T) {
	ls := newFakeLearningStore()
	ss := newFakeSessionStore()
	e := buildLearningEcho(ls, ss)

	rec := doLearningGet(e, "/api/v1/me/exports/csv", nil) // no cookie

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// 8. TestCSVExportOnlyOwnData — CSV contains only the requesting user's data
func TestCSVExportOnlyOwnData(t *testing.T) {
	ls := newFakeLearningStore()
	ss := newFakeSessionStore()

	// Enroll user-f and user-g in different paths
	ls.paths["path-A"] = &fakeLearningPath{ID: "path-A", Title: "Path A", IsPublished: true}
	ls.paths["path-B"] = &fakeLearningPath{ID: "path-B", Title: "Path B", IsPublished: true}

	e := buildLearningEcho(ls, ss)

	cookieF := makeLearningSession(ss, "user-f")
	cookieG := makeLearningSession(ss, "user-g")

	// Enroll user-f in path-A
	doLearningPost(e, "/api/v1/paths/path-A/enroll", nil, cookieF)
	// Enroll user-g in path-B
	doLearningPost(e, "/api/v1/paths/path-B/enroll", nil, cookieG)

	// user-f exports CSV — should only see path-A data, not path-B
	rec := doLearningGet(e, "/api/v1/me/exports/csv", cookieF)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/csv") {
		t.Errorf("expected text/csv content-type, got %q", ct)
	}

	csvBody := rec.Body.String()

	// user-f's enrollment (path-A) should appear
	if !strings.Contains(csvBody, "path-A") {
		t.Errorf("expected path-A in user-f's CSV export, got: %s", csvBody)
	}

	// user-g's enrollment (path-B) should NOT appear in user-f's export
	if strings.Contains(csvBody, "path-B") {
		t.Errorf("user-g's path-B must not appear in user-f's CSV export")
	}
}

// 9. TestRecordProgressRejectsUnenrolledResource — 422 if resource not in any enrolled path
func TestRecordProgressRejectsUnenrolledResource(t *testing.T) {
	ls := newFakeLearningStore()
	ss := newFakeSessionStore()

	// Create a path with specific resources but do NOT enroll the user.
	ls.paths["path-x"] = &fakeLearningPath{
		ID: "path-x", Title: "Path X", IsPublished: true,
		ResourceIDs: []string{"res-in-path"},
	}

	e := buildLearningEcho(ls, ss)
	cookie := makeLearningSession(ss, "user-unenrolled")

	// User is NOT enrolled in any path — recording progress should fail.
	body := map[string]any{
		"event_type":       "progress",
		"position_seconds": 60,
		"progress_pct":     25.0,
	}
	rec := doLearningPost(e, "/api/v1/me/progress/res-not-in-path", body, cookie)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for unenrolled resource, got %d — body: %s", rec.Code, rec.Body.String())
	}

	var errBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if errBody["code"] != "learning.resource_not_enrolled" {
		t.Errorf("expected code=learning.resource_not_enrolled, got %v", errBody["code"])
	}
}

// 10. TestRecordProgressRejectsResourceNotInEnrolledPath — enrolled but resource not in that path
func TestRecordProgressRejectsResourceNotInEnrolledPath(t *testing.T) {
	ls := newFakeLearningStore()
	ss := newFakeSessionStore()

	ls.paths["path-enrolled"] = &fakeLearningPath{
		ID: "path-enrolled", Title: "My Path", IsPublished: true,
		ResourceIDs: []string{"res-in-path"},
	}

	e := buildLearningEcho(ls, ss)
	cookie := makeLearningSession(ss, "user-enrolled-wrong")

	// Enroll user in the path.
	doLearningPost(e, "/api/v1/paths/path-enrolled/enroll", nil, cookie)

	// Try recording progress on a resource NOT in the enrolled path.
	body := map[string]any{
		"event_type":       "progress",
		"position_seconds": 30,
		"progress_pct":     10.0,
	}
	rec := doLearningPost(e, "/api/v1/me/progress/res-NOT-in-path", body, cookie)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d — body: %s", rec.Code, rec.Body.String())
	}
}
