// Package exports — HTTP handlers for export job management.
package exports

import (
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"portal/internal/app/common"
	"portal/internal/app/users"
)

// Handler exposes export job HTTP endpoints.
type Handler struct {
	store     *Store
	pool      *pgxpool.Pool
	userStore *users.Store
}

// NewHandler constructs a Handler.
func NewHandler(store *Store, pool *pgxpool.Pool, userStore *users.Store) *Handler {
	return &Handler{store: store, pool: pool, userStore: userStore}
}

// isAdmin checks whether the given userID has the "admin" role.
func (h *Handler) isAdmin(c echo.Context, userID string) bool {
	if h.userStore == nil {
		return false
	}
	uw, err := h.userStore.GetWithRoles(c.Request().Context(), userID)
	if err != nil {
		return false
	}
	for _, r := range uw.Roles {
		if r == "admin" {
			return true
		}
	}
	return false
}

// hasPermission checks whether the given userID holds a specific permission code.
func (h *Handler) hasPermission(c echo.Context, userID, perm string) bool {
	if h.userStore == nil {
		return false
	}
	uw, err := h.userStore.GetWithRoles(c.Request().Context(), userID)
	if err != nil {
		return false
	}
	for _, p := range uw.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}

// CreateJob creates a new export job.
// POST /api/v1/exports/jobs
func (h *Handler) CreateJob(c echo.Context) error {
	userID, ok := c.Get("user_id").(string)
	if !ok || userID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}

	var req struct {
		Type   string         `json:"type"`
		Params map[string]any `json:"params"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	if req.Type == "" {
		return common.BadRequest(c, "validation.required", "type is required")
	}

	// Validate allowed job types and enforce per-type authorization.
	// reconciliation_export is finance-only and must be gated by exports:write (seeded on finance role).
	// learning_progress_csv is permitted for any authenticated user but scoped to their own data.
	switch req.Type {
	case "learning_progress_csv":
		// valid — no additional permission gate; user can only export their own progress.
	case "reconciliation_export":
		if !h.hasPermission(c, userID, "exports:write") && !h.isAdmin(c, userID) {
			return common.Forbidden(c, "exports:write permission required")
		}
	default:
		return common.BadRequest(c, "validation.invalid_type",
			fmt.Sprintf("unknown job type: %s", req.Type))
	}

	if req.Params == nil {
		req.Params = map[string]any{}
	}

	// For learning_progress_csv, always use the caller's ID — ignore any client-supplied user_id.
	if req.Type == "learning_progress_csv" {
		req.Params["user_id"] = userID
	}

	// For reconciliation_export, scope to caller unless admin.
	// (The generator currently queries all runs; scoping enforced here at job creation.)
	if req.Type == "reconciliation_export" && !h.isAdmin(c, userID) {
		req.Params["created_by"] = userID
	}

	job, err := h.store.CreateJob(c.Request().Context(), req.Type, userID, req.Params)
	if err != nil {
		return common.Internal(c)
	}

	// The job is left in 'queued' status. The background worker (cmd/worker)
	// picks it up on its next 30s poll tick, processes it, and handles
	// retries + compensation if it fails. We do NOT start processing here
	// because:
	//   1. The worker's retry/compensation flow only applies to jobs it
	//      picked up itself. An API-side goroutine would move the job to
	//      'processing'→'failed' and the worker would never touch it again.
	//   2. Having two execution paths (API goroutine vs worker poll) creates
	//      a race window where both could claim the same job.
	// The worker uses FOR UPDATE SKIP LOCKED so concurrent polls are safe.

	return c.JSON(http.StatusCreated, job)
}

// ListJobs returns the authenticated user's export jobs.
// GET /api/v1/exports/jobs
func (h *Handler) ListJobs(c echo.Context) error {
	userID, ok := c.Get("user_id").(string)
	if !ok || userID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}

	limitStr := c.QueryParam("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	// Admin can see all; regular users see only their own.
	scopedBy := userID
	if h.isAdmin(c, userID) {
		scopedBy = ""
	}

	jobs, err := h.store.ListJobs(c.Request().Context(), scopedBy, limit)
	if err != nil {
		return common.Internal(c)
	}

	return c.JSON(http.StatusOK, map[string]any{"jobs": jobs})
}

// GetJob returns a single export job by ID.
// GET /api/v1/exports/jobs/:id
func (h *Handler) GetJob(c echo.Context) error {
	callerID, ok := c.Get("user_id").(string)
	if !ok || callerID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}

	jobID := c.Param("id")

	job, err := h.store.GetJob(c.Request().Context(), jobID)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "exports.not_found", "Export job not found")
	}

	// Owner/admin check
	if job.CreatedBy != callerID && !h.isAdmin(c, callerID) {
		return common.Forbidden(c, "Access denied")
	}

	return c.JSON(http.StatusOK, job)
}

// DownloadJob streams the completed export file.
// GET /api/v1/exports/jobs/:id/download
func (h *Handler) DownloadJob(c echo.Context) error {
	callerID, ok := c.Get("user_id").(string)
	if !ok || callerID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}

	jobID := c.Param("id")

	job, err := h.store.GetJob(c.Request().Context(), jobID)
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "exports.not_found", "Export job not found")
	}

	// Owner/admin check
	if job.CreatedBy != callerID && !h.isAdmin(c, callerID) {
		return common.Forbidden(c, "Access denied")
	}

	if job.Status != "completed" {
		return common.ErrorResponse(c, http.StatusConflict, "exports.not_ready",
			fmt.Sprintf("Export job is not completed (status: %s)", job.Status))
	}

	if job.FilePath == "" {
		return common.ErrorResponse(c, http.StatusNotFound, "exports.no_file", "Export file not available")
	}

	if _, err := os.Stat(job.FilePath); os.IsNotExist(err) {
		return common.ErrorResponse(c, http.StatusNotFound, "exports.file_missing", "Export file not found on disk")
	}

	filename := fmt.Sprintf("export-%s.csv", jobID)
	return c.Attachment(job.FilePath, filename)
}
