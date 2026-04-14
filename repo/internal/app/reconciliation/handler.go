package reconciliation

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"portal/internal/app/audit"
	"portal/internal/app/common"
)

// Handler handles HTTP endpoints for reconciliation and settlement.
type Handler struct {
	store *Store
	audit audit.Recorder // optional; settlement + variance + run state changes are audited when wired
}

// NewHandler constructs a Handler backed by the given store.
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// NewHandlerWithAudit returns a Handler that records every state-changing
// reconciliation/settlement action into the audit log.
func NewHandlerWithAudit(store *Store, recorder audit.Recorder) *Handler {
	return &Handler{store: store, audit: recorder}
}

func (h *Handler) recordAudit(c echo.Context, evt audit.Event) {
	if h.audit == nil {
		return
	}
	if evt.ActorID == "" {
		if uid, _ := c.Get("user_id").(string); uid != "" {
			evt.ActorID = uid
		}
	}
	if evt.IPAddress == "" {
		evt.IPAddress = c.RealIP()
	}
	h.audit.Record(c.Request().Context(), evt)
}

// ─────────────────────────────────────────────────────────────────────────────
// Statement imports
// ─────────────────────────────────────────────────────────────────────────────

// ImportStatements handles POST /api/v1/reconciliation/statements
// Body: { source_file, checksum, rows: [{order_id, line_description, statement_amount, currency, transaction_date}] }
func (h *Handler) ImportStatements(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	if userID == "" {
		return common.Unauthorized(c, "Authentication required")
	}

	var req struct {
		SourceFile string              `json:"source_file"`
		Checksum   string              `json:"checksum"`
		Rows       []StatementRowInput `json:"rows"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	if req.SourceFile == "" {
		return common.BadRequest(c, "validation.required", "source_file is required")
	}
	if len(req.Rows) == 0 {
		return common.BadRequest(c, "validation.required", "at least one row is required")
	}

	batch, err := h.store.ImportStatements(c.Request().Context(), userID,
		req.SourceFile, req.Checksum, req.Rows)
	if err != nil {
		return common.Internal(c)
	}

	h.recordAudit(c, audit.Event{
		Action:     "reconciliation.statements.import",
		Category:   "reconciliation",
		TargetType: "statement_import_batch",
		TargetID:   batch.ID,
		NewValue:   map[string]any{"source_file": req.SourceFile, "row_count": len(req.Rows)},
	})

	return c.JSON(http.StatusCreated, batch)
}

// ListImportBatches handles GET /api/v1/reconciliation/statements
func (h *Handler) ListImportBatches(c echo.Context) error {
	batches, err := h.store.ListImportBatches(c.Request().Context(), 50)
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, map[string]any{"batches": batches})
}

// ─────────────────────────────────────────────────────────────────────────────
// Billing rules
// ─────────────────────────────────────────────────────────────────────────────

// ListRules handles GET /api/v1/reconciliation/rules
func (h *Handler) ListRules(c echo.Context) error {
	rules, err := h.store.ListBillingRules(c.Request().Context())
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, map[string]any{"rules": rules})
}

// ─────────────────────────────────────────────────────────────────────────────
// Reconciliation runs
// ─────────────────────────────────────────────────────────────────────────────

// ListRuns handles GET /api/v1/reconciliation/runs
func (h *Handler) ListRuns(c echo.Context) error {
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	offset, _ := strconv.Atoi(c.QueryParam("offset"))
	if limit <= 0 {
		limit = 50
	}

	runs, total, err := h.store.ListRuns(c.Request().Context(), limit, offset)
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, map[string]any{
		"runs":   runs,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// CreateRun handles POST /api/v1/reconciliation/runs
func (h *Handler) CreateRun(c echo.Context) error {
	var req struct {
		Period string `json:"period"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	if req.Period == "" {
		return common.BadRequest(c, "validation.required", "period is required (format: YYYY-MM)")
	}

	userID, _ := c.Get("user_id").(string)
	run, err := h.store.CreateReconciliationRun(c.Request().Context(), req.Period, userID)
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusCreated, run)
}

// ProcessRun handles POST /api/v1/reconciliation/runs/:id/process
func (h *Handler) ProcessRun(c echo.Context) error {
	runID := c.Param("id")
	err := h.store.ProcessRun(c.Request().Context(), runID)
	if err != nil {
		if isNotFoundOrState(err) {
			return common.ErrorResponse(c, http.StatusConflict, "reconciliation.invalid_state", err.Error())
		}
		return common.Internal(c)
	}

	run, err := h.store.GetRun(c.Request().Context(), runID)
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, run)
}

// GetRun handles GET /api/v1/reconciliation/runs/:id
func (h *Handler) GetRun(c echo.Context) error {
	run, err := h.store.GetRun(c.Request().Context(), c.Param("id"))
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "reconciliation.not_found", "Run not found")
	}
	return c.JSON(http.StatusOK, run)
}

// ─────────────────────────────────────────────────────────────────────────────
// Variances
// ─────────────────────────────────────────────────────────────────────────────

// ListVariances handles GET /api/v1/reconciliation/runs/:id/variances
func (h *Handler) ListVariances(c echo.Context) error {
	runID := c.Param("id")
	status := c.QueryParam("status")

	variances, err := h.store.ListVariances(c.Request().Context(), runID, status)
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, map[string]any{"variances": variances})
}

// SubmitVarianceForApproval handles POST /api/v1/reconciliation/variances/:id/submit-approval
// Transitions a variance from 'open' to 'pending_finance_approval'.
func (h *Handler) SubmitVarianceForApproval(c echo.Context) error {
	varianceID := c.Param("id")
	userID, _ := c.Get("user_id").(string)

	err := h.store.SubmitVarianceForApproval(c.Request().Context(), varianceID, userID)
	if err != nil {
		if isNotFoundOrState(err) {
			return common.ErrorResponse(c, http.StatusConflict, "reconciliation.invalid_state", err.Error())
		}
		return common.Internal(c)
	}
	h.recordAudit(c, audit.Event{
		Action:     "reconciliation.variance.submit_for_approval",
		Category:   "reconciliation",
		TargetType: "variance",
		TargetID:   varianceID,
		NewValue:   map[string]any{"status": "pending_finance_approval"},
	})
	return c.JSON(http.StatusOK, map[string]string{"status": "pending_finance_approval"})
}

// ApproveVariance handles POST /api/v1/reconciliation/variances/:id/approve
// Requires reconciliation:read permission (Finance Analyst role).
// Transitions a variance from 'pending_finance_approval' to 'finance_approved'.
func (h *Handler) ApproveVariance(c echo.Context) error {
	varianceID := c.Param("id")
	userID, _ := c.Get("user_id").(string)

	err := h.store.ApproveVariance(c.Request().Context(), varianceID, userID)
	if err != nil {
		if isNotFoundOrState(err) {
			return common.ErrorResponse(c, http.StatusConflict, "reconciliation.invalid_state", err.Error())
		}
		return common.Internal(c)
	}
	h.recordAudit(c, audit.Event{
		Action:     "reconciliation.variance.finance_approve",
		Category:   "reconciliation",
		TargetType: "variance",
		TargetID:   varianceID,
		NewValue:   map[string]any{"status": "finance_approved", "approved_by": userID},
	})
	return c.JSON(http.StatusOK, map[string]string{"status": "finance_approved"})
}

// ApplySuggestion handles POST /api/v1/reconciliation/variances/:id/apply
// Now requires Finance approval — delegates to ApplyApprovedVariance.
func (h *Handler) ApplySuggestion(c echo.Context) error {
	varianceID := c.Param("id")
	err := h.store.ApplyApprovedVariance(c.Request().Context(), varianceID)
	if err != nil {
		return common.ErrorResponse(c, http.StatusConflict, "reconciliation.invalid_state", err.Error())
	}
	h.recordAudit(c, audit.Event{
		Action:     "reconciliation.variance.apply",
		Category:   "reconciliation",
		TargetType: "variance",
		TargetID:   varianceID,
		NewValue:   map[string]any{"status": "applied"},
	})
	return c.JSON(http.StatusOK, map[string]string{"status": "applied"})
}

// ─────────────────────────────────────────────────────────────────────────────
// Settlement batches
// ─────────────────────────────────────────────────────────────────────────────

// ListBatches handles GET /api/v1/reconciliation/batches
func (h *Handler) ListBatches(c echo.Context) error {
	runID := c.QueryParam("run_id")
	status := c.QueryParam("status")

	batches, err := h.store.ListSettlementBatches(c.Request().Context(), runID, status)
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, map[string]any{"batches": batches})
}

// CreateBatch handles POST /api/v1/reconciliation/batches
func (h *Handler) CreateBatch(c echo.Context) error {
	var req struct {
		RunID string               `json:"run_id"`
		Lines []SettlementLineInput `json:"lines"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	if req.RunID == "" {
		return common.BadRequest(c, "validation.required", "run_id is required")
	}
	if len(req.Lines) == 0 {
		return common.BadRequest(c, "validation.required", "at least one line is required")
	}

	userID, _ := c.Get("user_id").(string)
	batch, err := h.store.CreateSettlementBatch(c.Request().Context(), req.RunID, userID, req.Lines)
	if err != nil {
		if isValidationError(err) {
			return common.BadRequest(c, "validation.allocations", err.Error())
		}
		return common.Internal(c)
	}
	return c.JSON(http.StatusCreated, batch)
}

// GetBatch handles GET /api/v1/reconciliation/batches/:id
func (h *Handler) GetBatch(c echo.Context) error {
	batch, err := h.store.GetSettlementBatch(c.Request().Context(), c.Param("id"))
	if err != nil {
		return common.ErrorResponse(c, http.StatusNotFound, "reconciliation.not_found", "Batch not found")
	}
	return c.JSON(http.StatusOK, batch)
}

// SubmitBatch handles POST /api/v1/reconciliation/batches/:id/submit
func (h *Handler) SubmitBatch(c echo.Context) error {
	batchID := c.Param("id")
	err := h.store.SubmitForApproval(c.Request().Context(), batchID)
	if err != nil {
		return common.ErrorResponse(c, http.StatusConflict, "reconciliation.invalid_state", err.Error())
	}
	h.recordAudit(c, audit.Event{
		Action:     "settlement.batch.submit",
		Category:   "reconciliation",
		TargetType: "settlement_batch",
		TargetID:   batchID,
		NewValue:   map[string]any{"status": "under_review"},
	})
	batch, _ := h.store.GetSettlementBatch(c.Request().Context(), batchID)
	return c.JSON(http.StatusOK, batch)
}

// ApproveBatch handles POST /api/v1/reconciliation/batches/:id/approve
// Atomically approves the batch and generates AR/AP entries.
func (h *Handler) ApproveBatch(c echo.Context) error {
	userID, _ := c.Get("user_id").(string)
	batchID := c.Param("id")
	err := h.store.ApproveBatch(c.Request().Context(), batchID, userID)
	if err != nil {
		return common.ErrorResponse(c, http.StatusConflict, "reconciliation.invalid_state", err.Error())
	}
	h.recordAudit(c, audit.Event{
		Action:     "settlement.batch.approve",
		Category:   "reconciliation",
		TargetType: "settlement_batch",
		TargetID:   batchID,
		NewValue:   map[string]any{"status": "approved", "finance_approved_by": userID},
	})
	batch, _ := h.store.GetSettlementBatch(c.Request().Context(), batchID)
	return c.JSON(http.StatusOK, batch)
}

// ExportBatch handles POST /api/v1/reconciliation/batches/:id/export
func (h *Handler) ExportBatch(c echo.Context) error {
	batchID := c.Param("id")
	csvBytes, err := h.store.ExportBatch(c.Request().Context(), batchID)
	if err != nil {
		if isNotFoundOrState(err) {
			return common.ErrorResponse(c, http.StatusConflict, "reconciliation.invalid_state", err.Error())
		}
		return common.Internal(c)
	}

	h.recordAudit(c, audit.Event{
		Action:     "settlement.batch.export",
		Category:   "reconciliation",
		TargetType: "settlement_batch",
		TargetID:   batchID,
		NewValue:   map[string]any{"status": "exported", "size_bytes": len(csvBytes)},
	})

	c.Response().Header().Set("Content-Type", "text/csv; charset=utf-8")
	c.Response().Header().Set("Content-Disposition",
		`attachment; filename="settlement-batch-`+batchID+`.csv"`)
	return c.Blob(http.StatusOK, "text/csv; charset=utf-8", csvBytes)
}

// SettleBatch handles POST /api/v1/reconciliation/batches/:id/settle
func (h *Handler) SettleBatch(c echo.Context) error {
	batchID := c.Param("id")
	err := h.store.SettleBatch(c.Request().Context(), batchID)
	if err != nil {
		return common.ErrorResponse(c, http.StatusConflict, "reconciliation.invalid_state", err.Error())
	}
	h.recordAudit(c, audit.Event{
		Action:     "settlement.batch.settle",
		Category:   "reconciliation",
		TargetType: "settlement_batch",
		TargetID:   batchID,
		NewValue:   map[string]any{"status": "settled"},
	})
	batch, _ := h.store.GetSettlementBatch(c.Request().Context(), batchID)
	return c.JSON(http.StatusOK, batch)
}

// VoidBatch handles POST /api/v1/reconciliation/batches/:id/void
func (h *Handler) VoidBatch(c echo.Context) error {
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.Bind(&req)
	batchID := c.Param("id")

	err := h.store.VoidBatch(c.Request().Context(), batchID, req.Reason)
	if err != nil {
		return common.ErrorResponse(c, http.StatusConflict, "reconciliation.invalid_state", err.Error())
	}
	h.recordAudit(c, audit.Event{
		Action:     "settlement.batch.void",
		Category:   "reconciliation",
		TargetType: "settlement_batch",
		TargetID:   batchID,
		NewValue:   map[string]any{"status": "voided", "reason": req.Reason},
	})
	batch, _ := h.store.GetSettlementBatch(c.Request().Context(), batchID)
	return c.JSON(http.StatusOK, batch)
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func isNotFoundOrState(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "not found") ||
		contains(msg, "not in") ||
		contains(msg, "pending state") ||
		contains(msg, "cannot be voided") ||
		contains(msg, "must be in") ||
		contains(msg, "Finance approval is required")
}

func isValidationError(err error) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), "allocations must sum")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
