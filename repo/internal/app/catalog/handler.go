package catalog

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"portal/internal/app/common"
)

type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

func (h *Handler) ListResources(c echo.Context) error {
	opts := ListOptions{
		Filter: ListFilter{
			Category:    c.QueryParam("category"),
			ContentType: c.QueryParam("content_type"),
			TagCode:     c.QueryParam("tag"),
			FromDate:    c.QueryParam("from_date"),
			ToDate:      c.QueryParam("to_date"),
		},
		Sort:   c.QueryParam("sort"),
		Limit:  parseIntParam(c.QueryParam("limit"), 20),
		Offset: parseIntParam(c.QueryParam("offset"), 0),
	}

	resources, total, err := h.store.List(c.Request().Context(), opts)
	if err != nil {
		return common.Internal(c)
	}

	if resources == nil {
		resources = []Resource{}
	}

	return c.JSON(http.StatusOK, map[string]any{
		"resources": resources,
		"total":     total,
		"limit":     opts.Limit,
		"offset":    opts.Offset,
	})
}

func (h *Handler) GetResource(c echo.Context) error {
	id := c.Param("id")
	r, err := h.store.GetByID(c.Request().Context(), id)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "catalog.not_found", "Resource not found")
	}
	h.store.IncrementViewCount(c.Request().Context(), id)
	return c.JSON(http.StatusOK, r)
}

// resourceInput is the shared body shape for create/update.
type resourceInput struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	ContentType  string `json:"content_type"`
	Category     string `json:"category"`
	PublishDate  string `json:"publish_date"` // YYYY-MM-DD; empty means unset
	IsPublished  bool   `json:"is_published"`
}

// CreateResource handles POST /api/v1/catalog/resources.
// Requires catalog:write — see route registration in cmd/api/main.go.
func (h *Handler) CreateResource(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)

	var req resourceInput
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	if req.Title == "" {
		return common.BadRequest(c, "validation.required", "title is required")
	}
	if req.ContentType == "" {
		return common.BadRequest(c, "validation.required", "content_type is required")
	}
	if req.Category == "" {
		return common.BadRequest(c, "validation.required", "category is required")
	}

	r := Resource{
		Title:       req.Title,
		Description: req.Description,
		ContentType: req.ContentType,
		Category:    req.Category,
		IsPublished: req.IsPublished,
	}
	if req.PublishDate != "" {
		pd := req.PublishDate
		r.PublishDate = &pd
	}

	id, err := h.store.Create(c.Request().Context(), r, userID)
	if err != nil {
		return common.Internal(c)
	}

	out, err := h.store.GetByID(c.Request().Context(), id)
	if err != nil {
		// Created but cannot fetch — return the bare ID rather than 500.
		return c.JSON(http.StatusCreated, map[string]any{"id": id})
	}
	return c.JSON(http.StatusCreated, out)
}

// UpdateResource handles PUT /api/v1/catalog/resources/:id.
// Requires catalog:write.
func (h *Handler) UpdateResource(c echo.Context) error {
	id := c.Param("id")

	var req resourceInput
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}

	r := Resource{
		Title:       req.Title,
		Description: req.Description,
		ContentType: req.ContentType,
		Category:    req.Category,
		IsPublished: req.IsPublished,
	}
	if req.PublishDate != "" {
		pd := req.PublishDate
		r.PublishDate = &pd
	}

	if err := h.store.Update(c.Request().Context(), id, r); err != nil {
		if err.Error() == "resource not found" {
			return common.ErrorResponse(c, http.StatusNotFound, "catalog.not_found", "Resource not found")
		}
		return common.Internal(c)
	}

	out, err := h.store.GetByID(c.Request().Context(), id)
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, out)
}

// ArchiveResource handles POST /api/v1/catalog/resources/:id/archive.
// Soft-deletes the resource by setting is_archived=TRUE. Requires
// catalog:publish (the lifecycle permission held by content moderators).
func (h *Handler) ArchiveResource(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.Archive(c.Request().Context(), id); err != nil {
		if err.Error() == "resource not found" {
			return common.ErrorResponse(c, http.StatusNotFound, "catalog.not_found", "Resource not found")
		}
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "archived", "id": id})
}

// RestoreResource handles POST /api/v1/catalog/resources/:id/restore.
// Reverses ArchiveResource. Requires catalog:publish.
func (h *Handler) RestoreResource(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.Restore(c.Request().Context(), id); err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "catalog.not_found", "Resource not found")
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "restored", "id": id})
}

func parseIntParam(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return def
	}
	return v
}
