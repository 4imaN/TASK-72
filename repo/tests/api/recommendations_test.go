// tests/api/recommendations_test.go — HTTP-level tests for recommendation endpoints.
// All tests use httptest and in-process fakes; no real database required.
package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"portal/internal/app/common"
	"portal/internal/app/sessions"
)

// ── In-memory recommendations store ──────────────────────────────────────────

type fakeRecommendedItem struct {
	ResourceID  string              `json:"resource_id"`
	Title       string              `json:"title"`
	ContentType string              `json:"content_type"`
	Category    string              `json:"category"`
	Score       float64             `json:"score"`
	Factors     []fakeTraceFactor   `json:"factors"`
}

type fakeTraceFactor struct {
	Factor string  `json:"factor"`
	Weight float64 `json:"weight"`
	Label  string  `json:"label"`
}

type fakeRecStore struct {
	items  []fakeRecommendedItem
	events []map[string]string
}

func newFakeRecStore() *fakeRecStore {
	return &fakeRecStore{
		items:  []fakeRecommendedItem{},
		events: []map[string]string{},
	}
}

// buildRecommendationsEcho builds an Echo instance with recommendation routes
// backed by in-memory fakes.
func buildRecommendationsEcho(rs *fakeRecStore, ss *fakeSessionStore) *echo.Echo {
	e := echo.New()
	e.HideBanner = true

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

	// GET /api/v1/recommendations
	e.GET("/api/v1/recommendations", requireAuth(func(c echo.Context) error {
		items := rs.items
		if items == nil {
			items = []fakeRecommendedItem{}
		}
		return c.JSON(http.StatusOK, map[string]any{"items": items})
	}))

	// POST /api/v1/recommendations/events
	e.POST("/api/v1/recommendations/events", requireAuth(func(c echo.Context) error {
		var req struct {
			ResourceID string `json:"resource_id"`
			EventType  string `json:"event_type"`
		}
		if err := c.Bind(&req); err != nil {
			return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
		}

		switch req.EventType {
		case "view", "complete", "click":
			// valid
		default:
			return common.BadRequest(c, "validation.invalid_event_type",
				"event_type must be one of: view, complete, click")
		}

		if req.ResourceID == "" {
			return common.BadRequest(c, "validation.missing_resource_id", "resource_id is required")
		}

		rs.events = append(rs.events, map[string]string{
			"resource_id": req.ResourceID,
			"event_type":  req.EventType,
		})
		return c.NoContent(http.StatusNoContent)
	}))

	return e
}

// ── Test helpers ──────────────────────────────────────────────────────────────

func makeRecSession(ss *fakeSessionStore, userID string) *http.Cookie {
	token := "rectok_" + userID
	ss.sessions[token] = &sessions.Session{
		ID:     "recsess_" + userID,
		UserID: userID,
	}
	return &http.Cookie{
		Name:     sessions.CookieName,
		Value:    token,
		HttpOnly: true,
	}
}

func doRecGet(e *echo.Echo, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func doRecPost(e *echo.Echo, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// 1. GET /recommendations returns 200 with a valid session
func TestGetRecommendations_WithAuth_Returns200(t *testing.T) {
	rs := newFakeRecStore()
	ss := newFakeSessionStore()
	e := buildRecommendationsEcho(rs, ss)
	cookie := makeRecSession(ss, "user-rec-1")

	rec := doRecGet(e, "/api/v1/recommendations", cookie)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := body["items"]; !ok {
		t.Error("expected 'items' key in response")
	}
}

// 2. GET /recommendations returns 401 without session
func TestGetRecommendations_WithoutAuth_Returns401(t *testing.T) {
	rs := newFakeRecStore()
	ss := newFakeSessionStore()
	e := buildRecommendationsEcho(rs, ss)

	rec := doRecGet(e, "/api/v1/recommendations", nil) // no cookie

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// 3. POST /recommendations/events returns 204 with a valid event
func TestRecordEvent_ValidEvent_Returns204(t *testing.T) {
	rs := newFakeRecStore()
	ss := newFakeSessionStore()
	e := buildRecommendationsEcho(rs, ss)
	cookie := makeRecSession(ss, "user-rec-2")

	body := map[string]string{
		"resource_id": "resource-abc-123",
		"event_type":  "view",
	}
	rec := doRecPost(e, "/api/v1/recommendations/events", body, cookie)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d — body: %s", rec.Code, rec.Body.String())
	}

	if len(rs.events) != 1 {
		t.Errorf("expected 1 event recorded, got %d", len(rs.events))
	}
	if rs.events[0]["event_type"] != "view" {
		t.Errorf("expected event_type=view, got %s", rs.events[0]["event_type"])
	}
}

// 4. POST /recommendations/events returns 400 with invalid event_type
func TestRecordEvent_InvalidEventType_Returns400(t *testing.T) {
	rs := newFakeRecStore()
	ss := newFakeSessionStore()
	e := buildRecommendationsEcho(rs, ss)
	cookie := makeRecSession(ss, "user-rec-3")

	body := map[string]string{
		"resource_id": "resource-xyz",
		"event_type":  "purchase", // invalid
	}
	rec := doRecPost(e, "/api/v1/recommendations/events", body, cookie)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d — body: %s", rec.Code, rec.Body.String())
	}

	var errBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if errBody["code"] != "validation.invalid_event_type" {
		t.Errorf("expected code=validation.invalid_event_type, got %v", errBody["code"])
	}
}

// 5. GET /recommendations returns items array (may be empty for new user)
func TestGetRecommendations_ReturnsItemsArray(t *testing.T) {
	rs := newFakeRecStore()
	// Seed some recommendations
	rs.items = []fakeRecommendedItem{
		{
			ResourceID:  "res-1",
			Title:       "Leadership Fundamentals",
			ContentType: "course",
			Category:    "leadership",
			Score:       0.85,
			Factors: []fakeTraceFactor{
				{Factor: "job_family", Weight: 1.0, Label: "popular in your role"},
			},
		},
		{
			ResourceID:  "res-2",
			Title:       "Data Analysis Basics",
			ContentType: "article",
			Category:    "data",
			Score:       0.72,
			Factors: []fakeTraceFactor{
				{Factor: "tag_overlap", Weight: 0.5, Label: "matches your skills"},
			},
		},
	}
	ss := newFakeSessionStore()
	e := buildRecommendationsEcho(rs, ss)
	cookie := makeRecSession(ss, "user-rec-4")

	rec := doRecGet(e, "/api/v1/recommendations", cookie)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	items, ok := body["items"].([]any)
	if !ok {
		t.Fatal("expected items to be an array")
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}

	// Verify first item has required fields
	first, ok := items[0].(map[string]any)
	if !ok {
		t.Fatal("expected first item to be a map")
	}
	if first["resource_id"] != "res-1" {
		t.Errorf("expected resource_id=res-1, got %v", first["resource_id"])
	}
	if first["title"] == nil {
		t.Error("expected title to be present")
	}
	if _, hasFactor := first["factors"]; !hasFactor {
		t.Error("expected factors to be present in recommendation item")
	}
}

// 6. POST /recommendations/events with all valid event types
func TestRecordEvent_AllValidTypes(t *testing.T) {
	validTypes := []string{"view", "complete", "click"}

	for _, et := range validTypes {
		t.Run(et, func(t *testing.T) {
			rs := newFakeRecStore()
			ss := newFakeSessionStore()
			e := buildRecommendationsEcho(rs, ss)
			cookie := makeRecSession(ss, "user-type-test")

			body := map[string]string{
				"resource_id": "res-valid",
				"event_type":  et,
			}
			rec := doRecPost(e, "/api/v1/recommendations/events", body, cookie)

			if rec.Code != http.StatusNoContent {
				t.Errorf("event_type=%s: expected 204, got %d", et, rec.Code)
			}
		})
	}
}
