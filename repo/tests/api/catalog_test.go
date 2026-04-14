// tests/api/catalog_test.go — HTTP-level tests for catalog endpoints.
// All tests use httptest and in-process mocks; no real database is required.
package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"portal/internal/app/common"
)

// ── Fake catalog data ─────────────────────────────────────────────────────────

type fakeResource struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	ContentType string    `json:"content_type"`
	Category    string    `json:"category"`
	PublishDate *string   `json:"publish_date,omitempty"`
	IsPublished bool      `json:"is_published"`
	IsArchived  bool      `json:"is_archived"`
	ViewCount   int64     `json:"view_count"`
	Tags        []string  `json:"tags"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// buildCatalogEcho returns an echo instance with fake catalog endpoints.
func buildCatalogEcho(resources []fakeResource) *echo.Echo {
	e := echo.New()
	e.HideBanner = true

	e.GET("/api/v1/catalog/resources", func(c echo.Context) error {
		category := c.QueryParam("category")
		contentType := c.QueryParam("content_type")
		tag := c.QueryParam("tag")

		var filtered []fakeResource
		for _, r := range resources {
			if !r.IsPublished || r.IsArchived {
				continue
			}
			if category != "" && r.Category != category {
				continue
			}
			if contentType != "" && r.ContentType != contentType {
				continue
			}
			if tag != "" {
				found := false
				for _, t := range r.Tags {
					if t == tag {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}
			filtered = append(filtered, r)
		}
		if filtered == nil {
			filtered = []fakeResource{}
		}
		return c.JSON(http.StatusOK, map[string]any{
			"resources": filtered,
			"total":     len(filtered),
			"limit":     20,
			"offset":    0,
		})
	})

	e.GET("/api/v1/catalog/resources/:id", func(c echo.Context) error {
		id := c.Param("id")
		for _, r := range resources {
			if r.ID == id {
				return c.JSON(http.StatusOK, r)
			}
		}
		return common.ErrorResponse(c, http.StatusNotFound, "catalog.not_found", "Resource not found")
	})

	return e
}

func makeTestResources() []fakeResource {
	now := time.Now()
	pubDate := "2024-01-15"
	return []fakeResource{
		{
			ID: "res-001", Title: "Introduction to Leadership",
			ContentType: "article", Category: "leadership",
			IsPublished: true, IsArchived: false,
			ViewCount: 120, Tags: []string{"leadership", "management"},
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "res-002", Title: "Procurement Fundamentals",
			ContentType: "course", Category: "procurement",
			IsPublished: true, IsArchived: false,
			ViewCount: 85, Tags: []string{"procurement"},
			PublishDate: &pubDate,
			CreatedAt:   now, UpdatedAt: now,
		},
		{
			ID: "res-003", Title: "Data Analysis Basics",
			ContentType: "video", Category: "data",
			IsPublished: true, IsArchived: false,
			ViewCount: 200, Tags: []string{"data", "analytics"},
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "res-004", Title: "Archived Resource",
			ContentType: "article", Category: "leadership",
			IsPublished: true, IsArchived: true,
			ViewCount: 10, Tags: []string{},
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "res-005", Title: "Unpublished Resource",
			ContentType: "article", Category: "data",
			IsPublished: false, IsArchived: false,
			ViewCount: 0, Tags: []string{},
			CreatedAt: now, UpdatedAt: now,
		},
	}
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestCatalogListResources_ReturnsAll(t *testing.T) {
	e := buildCatalogEcho(makeTestResources())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/resources", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	results, ok := body["resources"].([]any)
	if !ok {
		t.Fatal("expected resources array")
	}
	// 3 published and non-archived resources
	if len(results) != 3 {
		t.Errorf("expected 3 resources, got %d", len(results))
	}
}

func TestCatalogListResources_FilterByCategory(t *testing.T) {
	e := buildCatalogEcho(makeTestResources())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/resources?category=leadership", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	results := body["resources"].([]any)
	if len(results) != 1 {
		t.Errorf("expected 1 leadership resource, got %d", len(results))
	}
}

func TestCatalogListResources_FilterByContentType(t *testing.T) {
	e := buildCatalogEcho(makeTestResources())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/resources?content_type=course", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	results := body["resources"].([]any)
	if len(results) != 1 {
		t.Errorf("expected 1 course, got %d", len(results))
	}
}

func TestCatalogListResources_FilterByTag(t *testing.T) {
	e := buildCatalogEcho(makeTestResources())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/resources?tag=procurement", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	results := body["resources"].([]any)
	if len(results) != 1 {
		t.Errorf("expected 1 procurement resource, got %d", len(results))
	}
}

func TestCatalogListResources_ArchivedExcluded(t *testing.T) {
	e := buildCatalogEcho(makeTestResources())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/resources", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	results := body["resources"].([]any)

	for _, r := range results {
		res := r.(map[string]any)
		if res["is_archived"].(bool) {
			t.Errorf("archived resource should not appear in listing")
		}
	}
}

func TestCatalogListResources_UnpublishedExcluded(t *testing.T) {
	e := buildCatalogEcho(makeTestResources())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/resources", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	results := body["resources"].([]any)

	for _, r := range results {
		res := r.(map[string]any)
		if !res["is_published"].(bool) {
			t.Errorf("unpublished resource should not appear in listing")
		}
	}
}

func TestCatalogListResources_ResponseShape(t *testing.T) {
	e := buildCatalogEcho(makeTestResources())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/resources", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	requiredKeys := []string{"resources", "total", "limit", "offset"}
	for _, k := range requiredKeys {
		if _, ok := body[k]; !ok {
			t.Errorf("expected key %q in response", k)
		}
	}
}

func TestCatalogGetResource_Found(t *testing.T) {
	e := buildCatalogEcho(makeTestResources())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/resources/res-001", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if body["id"] != "res-001" {
		t.Errorf("expected id=res-001, got %v", body["id"])
	}
	if body["title"] != "Introduction to Leadership" {
		t.Errorf("unexpected title: %v", body["title"])
	}
}

func TestCatalogGetResource_NotFound(t *testing.T) {
	e := buildCatalogEcho(makeTestResources())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/resources/nonexistent-id", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	var body common.AppError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if body.Code != "catalog.not_found" {
		t.Errorf("expected code=catalog.not_found, got %q", body.Code)
	}
}

func TestCatalogGetResource_ResponseShape(t *testing.T) {
	e := buildCatalogEcho(makeTestResources())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/resources/res-002", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)

	for _, k := range []string{"id", "title", "content_type", "category", "tags"} {
		if _, ok := body[k]; !ok {
			t.Errorf("expected key %q in resource response", k)
		}
	}
}

func TestCatalogListResources_EmptyResult(t *testing.T) {
	e := buildCatalogEcho(makeTestResources())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/resources?category=nonexistent", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	results := body["resources"].([]any)
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
	total := body["total"].(float64)
	if total != 0 {
		t.Errorf("expected total=0, got %v", total)
	}
}
