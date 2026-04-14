// tests/api/reconciliation_test.go — HTTP-level tests for reconciliation endpoints.
// Uses httptest and in-process fakes; no real database required.
package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"portal/internal/app/common"
	"portal/internal/app/sessions"
)

// ─────────────────────────────────────────────────────────────────────────────
// In-memory reconciliation fakes
// ─────────────────────────────────────────────────────────────────────────────

type fakeVariance struct {
	ID             string  `json:"id"`
	RunID          string  `json:"run_id"`
	VendorOrderID  string  `json:"vendor_order_id"`
	ExpectedAmount int64   `json:"expected_amount"`
	ActualAmount   int64   `json:"actual_amount"`
	Delta          int64   `json:"delta"`
	VarianceType   string  `json:"variance_type"`
	Suggestion     string  `json:"suggestion"`
	Status         string  `json:"status"`
}

type fakeRun struct {
	ID          string     `json:"id"`
	Period      string     `json:"period"`
	Status      string     `json:"status"`
	InitiatedBy string     `json:"initiated_by"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Variances   []*fakeVariance
}

type fakeAllocationInput struct {
	CostCenterID string   `json:"cost_center_id"`
	Percentage   *float64 `json:"percentage,omitempty"`
	Amount       int64    `json:"amount,omitempty"`
}

type fakeLineInput struct {
	VendorOrderID *string               `json:"vendor_order_id"`
	Amount        int64                 `json:"amount"`
	Direction     string                `json:"direction"`
	CostCenterID  string                `json:"cost_center_id"`
	Allocations   []fakeAllocationInput `json:"allocations,omitempty"`
}

type fakeLine struct {
	ID           string               `json:"id"`
	BatchID      string               `json:"batch_id"`
	VendorOrderID *string             `json:"vendor_order_id,omitempty"`
	Amount       int64                `json:"amount"`
	Direction    string               `json:"direction"`
	CostCenterID string               `json:"cost_center_id"`
}

type fakeBatch struct {
	ID                string     `json:"id"`
	RunID             string     `json:"run_id"`
	Status            string     `json:"status"`
	CreatedBy         string     `json:"created_by"`
	FinanceApprovedBy *string    `json:"finance_approved_by,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	ApprovedAt        *time.Time `json:"approved_at,omitempty"`
	ExportedAt        *time.Time `json:"exported_at,omitempty"`
	Lines             []fakeLine `json:"lines"`
}

type fakeBillingRule struct {
	ID            int64  `json:"id"`
	RuleSetName   string `json:"rule_set_name"`
	VersionNumber int    `json:"version_number"`
}

type fakeReconStore struct {
	rules    []fakeBillingRule
	runs     map[string]*fakeRun
	batches  map[string]*fakeBatch
	varIDSeq int
	runIDSeq int
	batIDSeq int
	lineSeq  int
}

func newFakeReconStore() *fakeReconStore {
	return &fakeReconStore{
		rules: []fakeBillingRule{
			{ID: 1, RuleSetName: "standard_vendor", VersionNumber: 1},
			{ID: 2, RuleSetName: "standard_vendor", VersionNumber: 2},
			{ID: 3, RuleSetName: "premium_vendor", VersionNumber: 1},
		},
		runs:    make(map[string]*fakeRun),
		batches: make(map[string]*fakeBatch),
	}
}

func (s *fakeReconStore) nextID(prefix string, seq *int) string {
	*seq++
	return fmt.Sprintf("%s-%d", prefix, *seq)
}

// ─────────────────────────────────────────────────────────────────────────────
// Echo wiring
// ─────────────────────────────────────────────────────────────────────────────

func buildReconEcho(rs *fakeReconStore, ss *fakeSessionStore) *echo.Echo {
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

	// GET /api/v1/reconciliation/rules
	e.GET("/api/v1/reconciliation/rules", requireAuth(func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"rules": rs.rules})
	}))

	// GET /api/v1/reconciliation/runs
	e.GET("/api/v1/reconciliation/runs", requireAuth(func(c echo.Context) error {
		var list []*fakeRun
		for _, r := range rs.runs {
			list = append(list, r)
		}
		if list == nil {
			list = []*fakeRun{}
		}
		return c.JSON(http.StatusOK, map[string]any{"runs": list, "total": len(list)})
	}))

	// POST /api/v1/reconciliation/runs
	e.POST("/api/v1/reconciliation/runs", requireAuth(func(c echo.Context) error {
		var req struct {
			Period string `json:"period"`
		}
		if err := c.Bind(&req); err != nil {
			return common.BadRequest(c, "validation.invalid_body", "Invalid body")
		}
		if req.Period == "" {
			return common.BadRequest(c, "validation.required", "period is required")
		}
		userID := c.Get("user_id").(string)
		id := rs.nextID("run", &rs.runIDSeq)
		r := &fakeRun{
			ID:          id,
			Period:      req.Period,
			Status:      "pending",
			InitiatedBy: userID,
			CreatedAt:   time.Now(),
		}
		rs.runs[id] = r
		return c.JSON(http.StatusCreated, r)
	}))

	// POST /api/v1/reconciliation/runs/:id/process
	e.POST("/api/v1/reconciliation/runs/:id/process", requireAuth(func(c echo.Context) error {
		id := c.Param("id")
		r, ok := rs.runs[id]
		if !ok {
			return common.ErrorResponse(c, http.StatusNotFound, "reconciliation.not_found", "Run not found")
		}
		if r.Status != "pending" {
			return common.ErrorResponse(c, http.StatusConflict, "reconciliation.invalid_state", "not in pending state")
		}

		// Simulate generating variances
		v1ID := rs.nextID("var", &rs.varIDSeq)
		r.Variances = append(r.Variances, &fakeVariance{
			ID:             v1ID,
			RunID:          id,
			VendorOrderID:  "order-abc",
			ExpectedAmount: 10000,
			ActualAmount:   10350,
			Delta:          350,
			VarianceType:   "amount",
			Suggestion:     "Overcharge of 350 minor units (3.5%). Recommend requesting credit note.",
			Status:         "open",
		})
		now := time.Now()
		r.Status = "completed"
		r.CompletedAt = &now
		return c.JSON(http.StatusOK, r)
	}))

	// GET /api/v1/reconciliation/runs/:id/variances
	e.GET("/api/v1/reconciliation/runs/:id/variances", requireAuth(func(c echo.Context) error {
		id := c.Param("id")
		r, ok := rs.runs[id]
		if !ok {
			return c.JSON(http.StatusOK, map[string]any{"variances": []any{}})
		}
		statusFilter := c.QueryParam("status")
		var out []*fakeVariance
		for _, v := range r.Variances {
			if statusFilter == "" || v.Status == statusFilter {
				out = append(out, v)
			}
		}
		if out == nil {
			out = []*fakeVariance{}
		}
		return c.JSON(http.StatusOK, map[string]any{"variances": out})
	}))

	// POST /api/v1/reconciliation/variances/:id/submit-approval
	e.POST("/api/v1/reconciliation/variances/:id/submit-approval", requireAuth(func(c echo.Context) error {
		varID := c.Param("id")
		for _, r := range rs.runs {
			for _, v := range r.Variances {
				if v.ID == varID {
					if v.Status != "open" {
						return common.ErrorResponse(c, http.StatusConflict,
							"reconciliation.invalid_state", "variance not in open state")
					}
					v.Status = "pending_finance_approval"
					return c.JSON(http.StatusOK, map[string]string{"status": "pending_finance_approval"})
				}
			}
		}
		return common.ErrorResponse(c, http.StatusNotFound, "reconciliation.not_found", "Variance not found")
	}))

	// POST /api/v1/reconciliation/variances/:id/approve
	e.POST("/api/v1/reconciliation/variances/:id/approve", requireAuth(func(c echo.Context) error {
		varID := c.Param("id")
		userID := c.Get("user_id").(string)
		for _, r := range rs.runs {
			for _, v := range r.Variances {
				if v.ID == varID {
					if v.Status != "pending_finance_approval" {
						return common.ErrorResponse(c, http.StatusConflict,
							"reconciliation.invalid_state", "variance not in pending_finance_approval state")
					}
					v.Status = "finance_approved"
					_ = userID // finance user recorded
					return c.JSON(http.StatusOK, map[string]string{"status": "finance_approved"})
				}
			}
		}
		return common.ErrorResponse(c, http.StatusNotFound, "reconciliation.not_found", "Variance not found")
	}))

	// POST /api/v1/reconciliation/variances/:id/apply
	e.POST("/api/v1/reconciliation/variances/:id/apply", requireAuth(func(c echo.Context) error {
		varID := c.Param("id")
		// Find variance across all runs — now requires finance_approved status
		for _, r := range rs.runs {
			for _, v := range r.Variances {
				if v.ID == varID {
					if v.Status != "finance_approved" {
						return common.ErrorResponse(c, http.StatusConflict,
							"reconciliation.invalid_state",
							"variance not in finance_approved state — Finance approval is required before applying")
					}
					v.Status = "applied"
					return c.JSON(http.StatusOK, map[string]string{"status": "applied"})
				}
			}
		}
		return common.ErrorResponse(c, http.StatusNotFound, "reconciliation.not_found", "Variance not found")
	}))

	// GET /api/v1/reconciliation/batches
	e.GET("/api/v1/reconciliation/batches", requireAuth(func(c echo.Context) error {
		var list []*fakeBatch
		for _, b := range rs.batches {
			list = append(list, b)
		}
		if list == nil {
			list = []*fakeBatch{}
		}
		return c.JSON(http.StatusOK, map[string]any{"batches": list})
	}))

	// POST /api/v1/reconciliation/batches
	e.POST("/api/v1/reconciliation/batches", requireAuth(func(c echo.Context) error {
		var req struct {
			RunID string          `json:"run_id"`
			Lines []fakeLineInput `json:"lines"`
		}
		if err := c.Bind(&req); err != nil {
			return common.BadRequest(c, "validation.invalid_body", "Invalid body")
		}
		if req.RunID == "" {
			return common.BadRequest(c, "validation.required", "run_id required")
		}
		if len(req.Lines) == 0 {
			return common.BadRequest(c, "validation.required", "lines required")
		}

		// Validate allocations
		for i, l := range req.Lines {
			if len(l.Allocations) == 0 {
				continue
			}
			var total float64
			for _, a := range l.Allocations {
				if a.Percentage != nil {
					total += *a.Percentage
				}
			}
			if total > 0 && (total < 99.99 || total > 100.01) {
				return common.BadRequest(c, "validation.allocations",
					fmt.Sprintf("line %d allocations must sum to 100%%", i))
			}
		}

		userID := c.Get("user_id").(string)
		id := rs.nextID("batch", &rs.batIDSeq)
		now := time.Now()
		b := &fakeBatch{
			ID:        id,
			RunID:     req.RunID,
			Status:    "draft",
			CreatedBy: userID,
			CreatedAt: now,
			UpdatedAt: now,
			Lines:     []fakeLine{},
		}
		for _, li := range req.Lines {
			lineID := rs.nextID("line", &rs.lineSeq)
			b.Lines = append(b.Lines, fakeLine{
				ID:           lineID,
				BatchID:      id,
				VendorOrderID: li.VendorOrderID,
				Amount:       li.Amount,
				Direction:    li.Direction,
				CostCenterID: li.CostCenterID,
			})
		}
		rs.batches[id] = b
		return c.JSON(http.StatusCreated, b)
	}))

	// GET /api/v1/reconciliation/batches/:id
	e.GET("/api/v1/reconciliation/batches/:id", requireAuth(func(c echo.Context) error {
		id := c.Param("id")
		b, ok := rs.batches[id]
		if !ok {
			return common.ErrorResponse(c, http.StatusNotFound, "reconciliation.not_found", "Batch not found")
		}
		return c.JSON(http.StatusOK, b)
	}))

	// POST /api/v1/reconciliation/batches/:id/submit
	e.POST("/api/v1/reconciliation/batches/:id/submit", requireAuth(func(c echo.Context) error {
		id := c.Param("id")
		b, ok := rs.batches[id]
		if !ok {
			return common.ErrorResponse(c, http.StatusNotFound, "reconciliation.not_found", "Batch not found")
		}
		if b.Status != "draft" {
			return common.ErrorResponse(c, http.StatusConflict,
				"reconciliation.invalid_state", "batch not in draft state")
		}
		b.Status = "under_review"
		b.UpdatedAt = time.Now()
		return c.JSON(http.StatusOK, b)
	}))

	// POST /api/v1/reconciliation/batches/:id/approve
	e.POST("/api/v1/reconciliation/batches/:id/approve", requireAuth(func(c echo.Context) error {
		id := c.Param("id")
		b, ok := rs.batches[id]
		if !ok {
			return common.ErrorResponse(c, http.StatusNotFound, "reconciliation.not_found", "Batch not found")
		}
		if b.Status != "under_review" {
			return common.ErrorResponse(c, http.StatusConflict,
				"reconciliation.invalid_state", "batch not in under_review state")
		}
		userID := c.Get("user_id").(string)
		now := time.Now()
		b.Status = "approved"
		b.FinanceApprovedBy = &userID
		b.ApprovedAt = &now
		b.UpdatedAt = now
		return c.JSON(http.StatusOK, b)
	}))

	// POST /api/v1/reconciliation/batches/:id/export
	e.POST("/api/v1/reconciliation/batches/:id/export", requireAuth(func(c echo.Context) error {
		id := c.Param("id")
		b, ok := rs.batches[id]
		if !ok {
			return common.ErrorResponse(c, http.StatusNotFound, "reconciliation.not_found", "Batch not found")
		}
		if b.Status != "approved" {
			return common.ErrorResponse(c, http.StatusConflict,
				"reconciliation.invalid_state", "batch must be approved to export")
		}
		now := time.Now()
		b.Status = "exported"
		b.ExportedAt = &now
		b.UpdatedAt = now

		// Build CSV
		var sb strings.Builder
		sb.WriteString("batch_id,line_id,vendor_order_id,amount,direction,cost_center_id,exported_at\n")
		for _, l := range b.Lines {
			orderID := ""
			if l.VendorOrderID != nil {
				orderID = *l.VendorOrderID
			}
			sb.WriteString(fmt.Sprintf("%s,%s,%s,%d,%s,%s,%s\n",
				id, l.ID, orderID, l.Amount, l.Direction, l.CostCenterID,
				now.UTC().Format(time.RFC3339)))
		}

		c.Response().Header().Set("Content-Type", "text/csv; charset=utf-8")
		c.Response().Header().Set("Content-Disposition",
			fmt.Sprintf(`attachment; filename="settlement-batch-%s.csv"`, id))
		return c.Blob(http.StatusOK, "text/csv; charset=utf-8", []byte(sb.String()))
	}))

	// POST /api/v1/reconciliation/batches/:id/settle
	e.POST("/api/v1/reconciliation/batches/:id/settle", requireAuth(func(c echo.Context) error {
		id := c.Param("id")
		b, ok := rs.batches[id]
		if !ok {
			return common.ErrorResponse(c, http.StatusNotFound, "reconciliation.not_found", "Batch not found")
		}
		if b.Status != "exported" {
			return common.ErrorResponse(c, http.StatusConflict,
				"reconciliation.invalid_state", "batch not in exported state")
		}
		b.Status = "settled"
		b.UpdatedAt = time.Now()
		return c.JSON(http.StatusOK, b)
	}))

	// POST /api/v1/reconciliation/batches/:id/void
	e.POST("/api/v1/reconciliation/batches/:id/void", requireAuth(func(c echo.Context) error {
		id := c.Param("id")
		b, ok := rs.batches[id]
		if !ok {
			return common.ErrorResponse(c, http.StatusNotFound, "reconciliation.not_found", "Batch not found")
		}
		allowed := b.Status == "draft" || b.Status == "under_review" || b.Status == "exception"
		if !allowed {
			return common.ErrorResponse(c, http.StatusConflict,
				"reconciliation.invalid_state", "batch cannot be voided from current state")
		}
		b.Status = "voided"
		b.UpdatedAt = time.Now()
		return c.JSON(http.StatusOK, b)
	}))

	return e
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP helpers
// ─────────────────────────────────────────────────────────────────────────────

func makeReconSession(ss *fakeSessionStore, userID string) *http.Cookie {
	token := "recon_tok_" + userID
	ss.sessions[token] = &sessions.Session{
		ID:     "recon_sess_" + userID,
		UserID: userID,
	}
	return &http.Cookie{
		Name:     sessions.CookieName,
		Value:    token,
		HttpOnly: true,
	}
}

func doReconGet(e *echo.Echo, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func doReconPost(e *echo.Echo, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
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

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// 1. GET /reconciliation/rules returns 200
func TestListBillingRulesOK(t *testing.T) {
	rs := newFakeReconStore()
	ss := newFakeSessionStore()
	e := buildReconEcho(rs, ss)
	cookie := makeReconSession(ss, "user-finance-1")

	rec := doReconGet(e, "/api/v1/reconciliation/rules", cookie)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	rules, ok := body["rules"].([]any)
	if !ok {
		t.Fatal("expected rules array")
	}
	if len(rules) != 3 {
		t.Errorf("expected 3 billing rules, got %d", len(rules))
	}
}

// 2. POST /reconciliation/runs creates a run
func TestCreateReconciliationRun(t *testing.T) {
	rs := newFakeReconStore()
	ss := newFakeSessionStore()
	e := buildReconEcho(rs, ss)
	cookie := makeReconSession(ss, "user-finance-2")

	rec := doReconPost(e, "/api/v1/reconciliation/runs",
		map[string]string{"period": "2026-03"}, cookie)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — %s", rec.Code, rec.Body.String())
	}
	var run map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &run)
	if run["period"] != "2026-03" {
		t.Errorf("expected period=2026-03, got %v", run["period"])
	}
	if run["status"] != "pending" {
		t.Errorf("expected status=pending, got %v", run["status"])
	}
}

// 3. POST /reconciliation/runs/:id/process processes the run
func TestProcessReconciliationRun(t *testing.T) {
	rs := newFakeReconStore()
	ss := newFakeSessionStore()
	e := buildReconEcho(rs, ss)
	cookie := makeReconSession(ss, "user-finance-3")

	// Create a run first
	createRec := doReconPost(e, "/api/v1/reconciliation/runs",
		map[string]string{"period": "2026-02"}, cookie)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create run failed: %d", createRec.Code)
	}
	var run map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &run)
	runID := run["id"].(string)

	// Process it
	rec := doReconPost(e, "/api/v1/reconciliation/runs/"+runID+"/process", nil, cookie)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rec.Code, rec.Body.String())
	}
	var processed map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &processed)
	if processed["status"] != "completed" {
		t.Errorf("expected status=completed, got %v", processed["status"])
	}
}

// 4. GET /reconciliation/runs/:id/variances returns variances list
func TestListVariances(t *testing.T) {
	rs := newFakeReconStore()
	ss := newFakeSessionStore()
	e := buildReconEcho(rs, ss)
	cookie := makeReconSession(ss, "user-finance-4")

	// Create + process a run
	createRec := doReconPost(e, "/api/v1/reconciliation/runs",
		map[string]string{"period": "2026-01"}, cookie)
	var run map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &run)
	runID := run["id"].(string)
	doReconPost(e, "/api/v1/reconciliation/runs/"+runID+"/process", nil, cookie)

	// List variances
	rec := doReconGet(e, "/api/v1/reconciliation/runs/"+runID+"/variances", cookie)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	variances, ok := body["variances"].([]any)
	if !ok {
		t.Fatal("expected variances array")
	}
	// Our fake generates 1 variance after processing
	if len(variances) != 1 {
		t.Errorf("expected 1 variance, got %d", len(variances))
	}
}

// 5. Finance approval flow: submit → approve → apply (full gate test)
func TestApplySuggestion(t *testing.T) {
	rs := newFakeReconStore()
	ss := newFakeSessionStore()
	e := buildReconEcho(rs, ss)
	cookie := makeReconSession(ss, "user-finance-5")

	// Create, process run to get a variance
	createRec := doReconPost(e, "/api/v1/reconciliation/runs",
		map[string]string{"period": "2025-12"}, cookie)
	var run map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &run)
	runID := run["id"].(string)
	doReconPost(e, "/api/v1/reconciliation/runs/"+runID+"/process", nil, cookie)

	// Get variances to find the ID
	varRec := doReconGet(e, "/api/v1/reconciliation/runs/"+runID+"/variances", cookie)
	var varBody map[string]any
	_ = json.Unmarshal(varRec.Body.Bytes(), &varBody)
	variances := varBody["variances"].([]any)
	if len(variances) == 0 {
		t.Skip("no variances to apply — run produced no variances")
	}
	varID := variances[0].(map[string]any)["id"].(string)

	// Apply without Finance approval must fail (status is 'open').
	rec := doReconPost(e, "/api/v1/reconciliation/variances/"+varID+"/apply", nil, cookie)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 before Finance approval, got %d — %s", rec.Code, rec.Body.String())
	}

	// Submit for approval.
	submitRec := doReconPost(e, "/api/v1/reconciliation/variances/"+varID+"/submit-approval", nil, cookie)
	if submitRec.Code != http.StatusOK {
		t.Fatalf("submit-approval: expected 200, got %d — %s", submitRec.Code, submitRec.Body.String())
	}
	var submitResult map[string]any
	_ = json.Unmarshal(submitRec.Body.Bytes(), &submitResult)
	if submitResult["status"] != "pending_finance_approval" {
		t.Errorf("expected status=pending_finance_approval, got %v", submitResult["status"])
	}

	// Apply still fails — not yet finance_approved.
	rec2 := doReconPost(e, "/api/v1/reconciliation/variances/"+varID+"/apply", nil, cookie)
	if rec2.Code != http.StatusConflict {
		t.Errorf("expected 409 while pending_finance_approval, got %d", rec2.Code)
	}

	// Finance approves.
	approveRec := doReconPost(e, "/api/v1/reconciliation/variances/"+varID+"/approve", nil, cookie)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve: expected 200, got %d — %s", approveRec.Code, approveRec.Body.String())
	}
	var approveResult map[string]any
	_ = json.Unmarshal(approveRec.Body.Bytes(), &approveResult)
	if approveResult["status"] != "finance_approved" {
		t.Errorf("expected status=finance_approved, got %v", approveResult["status"])
	}

	// Now apply succeeds.
	applyRec := doReconPost(e, "/api/v1/reconciliation/variances/"+varID+"/apply", nil, cookie)
	if applyRec.Code != http.StatusOK {
		t.Fatalf("apply: expected 200, got %d — %s", applyRec.Code, applyRec.Body.String())
	}
	var result map[string]any
	_ = json.Unmarshal(applyRec.Body.Bytes(), &result)
	if result["status"] != "applied" {
		t.Errorf("expected status=applied, got %v", result["status"])
	}
}

// 5b. Variance cannot be approved before it's submitted for approval.
func TestApproveVarianceRequiresPendingState(t *testing.T) {
	rs := newFakeReconStore()
	ss := newFakeSessionStore()
	e := buildReconEcho(rs, ss)
	cookie := makeReconSession(ss, "user-finance-5b")

	createRec := doReconPost(e, "/api/v1/reconciliation/runs",
		map[string]string{"period": "2025-11"}, cookie)
	var run map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &run)
	runID := run["id"].(string)
	doReconPost(e, "/api/v1/reconciliation/runs/"+runID+"/process", nil, cookie)

	varRec := doReconGet(e, "/api/v1/reconciliation/runs/"+runID+"/variances", cookie)
	var varBody map[string]any
	_ = json.Unmarshal(varRec.Body.Bytes(), &varBody)
	variances := varBody["variances"].([]any)
	if len(variances) == 0 {
		t.Skip("no variances produced")
	}
	varID := variances[0].(map[string]any)["id"].(string)

	// Attempt to finance-approve without submitting first — should conflict.
	rec := doReconPost(e, "/api/v1/reconciliation/variances/"+varID+"/approve", nil, cookie)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 when approving without prior submission, got %d — %s", rec.Code, rec.Body.String())
	}
}

// 5c. Duplicate submit-approval is rejected.
func TestSubmitVarianceForApprovalIdempotencyRejected(t *testing.T) {
	rs := newFakeReconStore()
	ss := newFakeSessionStore()
	e := buildReconEcho(rs, ss)
	cookie := makeReconSession(ss, "user-finance-5c")

	createRec := doReconPost(e, "/api/v1/reconciliation/runs",
		map[string]string{"period": "2025-10"}, cookie)
	var run map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &run)
	runID := run["id"].(string)
	doReconPost(e, "/api/v1/reconciliation/runs/"+runID+"/process", nil, cookie)

	varRec := doReconGet(e, "/api/v1/reconciliation/runs/"+runID+"/variances", cookie)
	var varBody map[string]any
	_ = json.Unmarshal(varRec.Body.Bytes(), &varBody)
	variances := varBody["variances"].([]any)
	if len(variances) == 0 {
		t.Skip("no variances produced")
	}
	varID := variances[0].(map[string]any)["id"].(string)

	// First submit — succeeds.
	first := doReconPost(e, "/api/v1/reconciliation/variances/"+varID+"/submit-approval", nil, cookie)
	if first.Code != http.StatusOK {
		t.Fatalf("first submit: expected 200, got %d", first.Code)
	}

	// Second submit — should fail (no longer 'open').
	second := doReconPost(e, "/api/v1/reconciliation/variances/"+varID+"/submit-approval", nil, cookie)
	if second.Code != http.StatusConflict {
		t.Errorf("second submit: expected 409, got %d — %s", second.Code, second.Body.String())
	}
}

// 6. POST /reconciliation/batches creates a batch
func TestCreateSettlementBatch(t *testing.T) {
	rs := newFakeReconStore()
	ss := newFakeSessionStore()
	e := buildReconEcho(rs, ss)
	cookie := makeReconSession(ss, "user-finance-6")

	// Create a run first
	createRec := doReconPost(e, "/api/v1/reconciliation/runs",
		map[string]string{"period": "2025-11"}, cookie)
	var run map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &run)
	runID := run["id"].(string)

	rec := doReconPost(e, "/api/v1/reconciliation/batches",
		map[string]any{
			"run_id": runID,
			"lines": []map[string]any{
				{
					"amount":         50000,
					"direction":      "AP",
					"cost_center_id": "CC-001",
				},
			},
		}, cookie)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — %s", rec.Code, rec.Body.String())
	}
	var batch map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &batch)
	if batch["status"] != "draft" {
		t.Errorf("expected status=draft, got %v", batch["status"])
	}
	lines, ok := batch["lines"].([]any)
	if !ok || len(lines) != 1 {
		t.Errorf("expected 1 line, got %v", batch["lines"])
	}
}

// 7. POST /reconciliation/batches/:id/submit transitions to under_review
func TestSubmitBatchForApproval(t *testing.T) {
	rs := newFakeReconStore()
	ss := newFakeSessionStore()
	e := buildReconEcho(rs, ss)
	cookie := makeReconSession(ss, "user-finance-7")

	// Create run + batch
	createRunRec := doReconPost(e, "/api/v1/reconciliation/runs",
		map[string]string{"period": "2025-10"}, cookie)
	var run map[string]any
	_ = json.Unmarshal(createRunRec.Body.Bytes(), &run)
	runID := run["id"].(string)

	createBatchRec := doReconPost(e, "/api/v1/reconciliation/batches",
		map[string]any{
			"run_id": runID,
			"lines":  []map[string]any{{"amount": 10000, "direction": "AP", "cost_center_id": "CC-002"}},
		}, cookie)
	var batch map[string]any
	_ = json.Unmarshal(createBatchRec.Body.Bytes(), &batch)
	batchID := batch["id"].(string)

	rec := doReconPost(e, "/api/v1/reconciliation/batches/"+batchID+"/submit", nil, cookie)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rec.Code, rec.Body.String())
	}
	var updated map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &updated)
	if updated["status"] != "under_review" {
		t.Errorf("expected status=under_review, got %v", updated["status"])
	}
}

// 8. POST /reconciliation/batches/:id/approve transitions to approved
func TestApproveBatch(t *testing.T) {
	rs := newFakeReconStore()
	ss := newFakeSessionStore()
	e := buildReconEcho(rs, ss)
	cookie := makeReconSession(ss, "user-finance-8")

	// Create run → batch → submit → approve
	createRunRec := doReconPost(e, "/api/v1/reconciliation/runs",
		map[string]string{"period": "2025-09"}, cookie)
	var run map[string]any
	_ = json.Unmarshal(createRunRec.Body.Bytes(), &run)
	runID := run["id"].(string)

	createBatchRec := doReconPost(e, "/api/v1/reconciliation/batches",
		map[string]any{
			"run_id": runID,
			"lines":  []map[string]any{{"amount": 20000, "direction": "AR", "cost_center_id": "CC-003"}},
		}, cookie)
	var batch map[string]any
	_ = json.Unmarshal(createBatchRec.Body.Bytes(), &batch)
	batchID := batch["id"].(string)

	doReconPost(e, "/api/v1/reconciliation/batches/"+batchID+"/submit", nil, cookie)

	rec := doReconPost(e, "/api/v1/reconciliation/batches/"+batchID+"/approve", nil, cookie)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rec.Code, rec.Body.String())
	}
	var approved map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &approved)
	if approved["status"] != "approved" {
		t.Errorf("expected status=approved, got %v", approved["status"])
	}
	if approved["finance_approved_by"] == nil {
		t.Error("expected finance_approved_by to be set")
	}
}

// 9. POST /reconciliation/batches/:id/export returns CSV download
func TestExportBatch(t *testing.T) {
	rs := newFakeReconStore()
	ss := newFakeSessionStore()
	e := buildReconEcho(rs, ss)
	cookie := makeReconSession(ss, "user-finance-9")

	// Full workflow: create → batch → submit → approve → export
	createRunRec := doReconPost(e, "/api/v1/reconciliation/runs",
		map[string]string{"period": "2025-08"}, cookie)
	var run map[string]any
	_ = json.Unmarshal(createRunRec.Body.Bytes(), &run)
	runID := run["id"].(string)

	orderID := "order-test-001"
	createBatchRec := doReconPost(e, "/api/v1/reconciliation/batches",
		map[string]any{
			"run_id": runID,
			"lines": []map[string]any{
				{"vendor_order_id": orderID, "amount": 75000, "direction": "AP", "cost_center_id": "CC-ENG"},
			},
		}, cookie)
	var batch map[string]any
	_ = json.Unmarshal(createBatchRec.Body.Bytes(), &batch)
	batchID := batch["id"].(string)

	doReconPost(e, "/api/v1/reconciliation/batches/"+batchID+"/submit", nil, cookie)
	doReconPost(e, "/api/v1/reconciliation/batches/"+batchID+"/approve", nil, cookie)

	rec := doReconPost(e, "/api/v1/reconciliation/batches/"+batchID+"/export", nil, cookie)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/csv") {
		t.Errorf("expected text/csv content type, got %q", ct)
	}
	csvBody := rec.Body.String()
	if !strings.Contains(csvBody, "batch_id") {
		t.Error("expected CSV header 'batch_id' in output")
	}
	if !strings.Contains(csvBody, batchID) {
		t.Errorf("expected batch_id %s in CSV, got: %s", batchID, csvBody)
	}
	if !strings.Contains(csvBody, orderID) {
		t.Errorf("expected order_id %s in CSV, got: %s", orderID, csvBody)
	}
}

// 10. POST /reconciliation/batches/:id/void voids a draft batch
func TestVoidDraftBatch(t *testing.T) {
	rs := newFakeReconStore()
	ss := newFakeSessionStore()
	e := buildReconEcho(rs, ss)
	cookie := makeReconSession(ss, "user-finance-10")

	createRunRec := doReconPost(e, "/api/v1/reconciliation/runs",
		map[string]string{"period": "2025-07"}, cookie)
	var run map[string]any
	_ = json.Unmarshal(createRunRec.Body.Bytes(), &run)
	runID := run["id"].(string)

	createBatchRec := doReconPost(e, "/api/v1/reconciliation/batches",
		map[string]any{
			"run_id": runID,
			"lines":  []map[string]any{{"amount": 5000, "direction": "AP", "cost_center_id": "CC-FIN"}},
		}, cookie)
	var batch map[string]any
	_ = json.Unmarshal(createBatchRec.Body.Bytes(), &batch)
	batchID := batch["id"].(string)

	rec := doReconPost(e, "/api/v1/reconciliation/batches/"+batchID+"/void",
		map[string]string{"reason": "Duplicate entry"}, cookie)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", rec.Code, rec.Body.String())
	}
	var voided map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &voided)
	if voided["status"] != "voided" {
		t.Errorf("expected status=voided, got %v", voided["status"])
	}
}

// 11. GET /reconciliation/runs returns 401 without session
func TestListRunsRequiresAuth(t *testing.T) {
	rs := newFakeReconStore()
	ss := newFakeSessionStore()
	e := buildReconEcho(rs, ss)

	rec := doReconGet(e, "/api/v1/reconciliation/runs", nil) // no cookie

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// Allocation validation: allocations not summing to 100% returns 400
func TestCreateBatchAllocationValidation(t *testing.T) {
	rs := newFakeReconStore()
	ss := newFakeSessionStore()
	e := buildReconEcho(rs, ss)
	cookie := makeReconSession(ss, "user-finance-11")

	createRunRec := doReconPost(e, "/api/v1/reconciliation/runs",
		map[string]string{"period": "2025-06"}, cookie)
	var run map[string]any
	_ = json.Unmarshal(createRunRec.Body.Bytes(), &run)
	runID := run["id"].(string)

	pct60 := 60.0
	pct20 := 20.0

	rec := doReconPost(e, "/api/v1/reconciliation/batches",
		map[string]any{
			"run_id": runID,
			"lines": []map[string]any{
				{
					"amount":         10000,
					"direction":      "AP",
					"cost_center_id": "CC-FIN",
					"allocations": []map[string]any{
						{"cost_center_id": "CC-A", "percentage": pct60},
						{"cost_center_id": "CC-B", "percentage": pct20},
						// Total = 80%, not 100%
					},
				},
			},
		}, cookie)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid allocations, got %d — %s", rec.Code, rec.Body.String())
	}
}

// Full settlement lifecycle test: draft → under_review → approved → exported → settled
func TestFullSettlementLifecycle(t *testing.T) {
	rs := newFakeReconStore()
	ss := newFakeSessionStore()
	e := buildReconEcho(rs, ss)
	cookie := makeReconSession(ss, "user-finance-12")

	// Create run
	createRunRec := doReconPost(e, "/api/v1/reconciliation/runs",
		map[string]string{"period": "2025-05"}, cookie)
	var run map[string]any
	_ = json.Unmarshal(createRunRec.Body.Bytes(), &run)
	runID := run["id"].(string)

	// Create batch
	createBatchRec := doReconPost(e, "/api/v1/reconciliation/batches",
		map[string]any{
			"run_id": runID,
			"lines":  []map[string]any{{"amount": 30000, "direction": "AP", "cost_center_id": "CC-OPS"}},
		}, cookie)
	if createBatchRec.Code != http.StatusCreated {
		t.Fatalf("create batch: got %d", createBatchRec.Code)
	}
	var batch map[string]any
	_ = json.Unmarshal(createBatchRec.Body.Bytes(), &batch)
	batchID := batch["id"].(string)

	// Submit
	submitRec := doReconPost(e, "/api/v1/reconciliation/batches/"+batchID+"/submit", nil, cookie)
	if submitRec.Code != http.StatusOK {
		t.Fatalf("submit: got %d — %s", submitRec.Code, submitRec.Body.String())
	}

	// Approve
	approveRec := doReconPost(e, "/api/v1/reconciliation/batches/"+batchID+"/approve", nil, cookie)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve: got %d — %s", approveRec.Code, approveRec.Body.String())
	}

	// Export
	exportRec := doReconPost(e, "/api/v1/reconciliation/batches/"+batchID+"/export", nil, cookie)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export: got %d — %s", exportRec.Code, exportRec.Body.String())
	}
	if !strings.Contains(exportRec.Header().Get("Content-Type"), "text/csv") {
		t.Error("export should return text/csv")
	}

	// After export, batch should be 'exported' — verify with GET
	getRec := doReconGet(e, "/api/v1/reconciliation/batches/"+batchID, cookie)
	var exported map[string]any
	_ = json.Unmarshal(getRec.Body.Bytes(), &exported)
	if exported["status"] != "exported" {
		t.Errorf("expected status=exported after export, got %v", exported["status"])
	}

	// Settle
	settleRec := doReconPost(e, "/api/v1/reconciliation/batches/"+batchID+"/settle", nil, cookie)
	if settleRec.Code != http.StatusOK {
		t.Fatalf("settle: got %d — %s", settleRec.Code, settleRec.Body.String())
	}
	var settled map[string]any
	_ = json.Unmarshal(settleRec.Body.Bytes(), &settled)
	if settled["status"] != "settled" {
		t.Errorf("expected status=settled, got %v", settled["status"])
	}
}
