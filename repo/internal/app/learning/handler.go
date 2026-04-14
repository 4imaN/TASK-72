package learning

import (
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"portal/internal/app/common"
	"portal/internal/platform/logging"
)

type Handler struct {
	store *Store
	log   *logging.Logger
}

func NewHandler(store *Store, log *logging.Logger) *Handler {
	return &Handler{store: store, log: log}
}

func (h *Handler) ListPaths(c echo.Context) error {
	paths, err := h.store.ListPaths(c.Request().Context())
	if err != nil {
		return common.Internal(c)
	}
	if paths == nil {
		paths = []LearningPath{}
	}
	return c.JSON(http.StatusOK, map[string]any{"paths": paths})
}

func (h *Handler) GetPath(c echo.Context) error {
	pathID := c.Param("id")
	path, err := h.store.GetPath(c.Request().Context(), pathID)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "learning.not_found", "Path not found")
	}
	return c.JSON(http.StatusOK, path)
}

func (h *Handler) Enroll(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}
	pathID := c.Param("id")

	enrollment, err := h.store.Enroll(c.Request().Context(), userID, pathID)
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, enrollment)
}

func (h *Handler) GetPathProgress(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}
	pathID := c.Param("id")

	pp, err := h.store.GetPathProgress(c.Request().Context(), userID, pathID)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "learning.not_enrolled", "Not enrolled in this path")
	}
	return c.JSON(http.StatusOK, pp)
}

func (h *Handler) RecordProgress(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}
	resourceID := c.Param("resource_id")

	var req struct {
		EventType       string  `json:"event_type"`
		PositionSeconds int     `json:"position_seconds"`
		ProgressPct     float64 `json:"progress_pct"`
		DeviceHint      string  `json:"device_hint"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	if req.EventType == "" {
		req.EventType = "progress"
	}

	if err := h.store.RecordProgress(c.Request().Context(), userID, resourceID,
		req.EventType, req.PositionSeconds, req.ProgressPct, req.DeviceHint); err != nil {
		if err.Error() == "resource not in any enrolled path" {
			return common.ErrorResponse(c, http.StatusUnprocessableEntity,
				"learning.resource_not_enrolled",
				"Resource does not belong to any learning path you are enrolled in")
		}
		return common.Internal(c)
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// ListEnrollments returns the caller's enrolled learning paths with per-path
// progress summary. GET /api/v1/me/enrollments
func (h *Handler) ListEnrollments(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}
	enrollments, err := h.store.ListEnrollments(c.Request().Context(), userID)
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, map[string]any{"enrollments": enrollments})
}

func (h *Handler) GetResumeState(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}

	state, err := h.store.GetResumeState(c.Request().Context(), userID)
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, map[string]any{"in_progress": state})
}

func (h *Handler) ExportCSV(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}

	filename := fmt.Sprintf("learning-record-%s.csv", time.Now().Format("2006-01-02"))
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Response().Header().Set("Content-Type", "text/csv; charset=utf-8")

	if err := h.store.GenerateCSV(c.Request().Context(), userID, c.Response().Writer); err != nil {
		h.log.Error("generate csv", map[string]any{"err": err.Error(), "user_id": userID})
		// Headers already sent; can't return JSON error
		return nil
	}
	return nil
}
