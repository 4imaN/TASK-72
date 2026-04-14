// tests/integration/finance_flow_test.go — exercises the end-to-end
// statement-import → reconciliation-run → variance-approval flow against a
// real PostgreSQL database with the full middleware chain.
package integration_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	appconfig "portal/internal/app/config"
	"portal/internal/app/audit"
	"portal/internal/app/exports"
	"portal/internal/app/mfa"
	"portal/internal/app/permissions"
	"portal/internal/app/reconciliation"
	"portal/internal/app/sessions"
	"portal/internal/app/users"
)

// TestStatementImportAndReconciliation drives the full finance workflow:
//
//  1. Import a statement batch via POST /reconciliation/statements.
//  2. Create a reconciliation run for the same period.
//  3. Process the run — variances are produced from real statement data.
//  4. Verify the run completed and variances were created.
//  5. Read-only user is forbidden from importing (route gate).
func TestStatementImportAndReconciliation(t *testing.T) {
	h := Setup(t)
	defer h.Cleanup()
	ctx := context.Background()

	auditStore := audit.NewStore(h.Pool)
	userStore  := users.NewStore(h.Pool)
	sessStore  := sessions.NewStore(h.Pool)
	mfaStore   := mfa.NewStore(h.Pool, nil)
	cfgStore   := appconfig.NewStore(h.Pool)
	reconStore := reconciliation.NewStore(h.Pool)
	reconHandler := reconciliation.NewHandlerWithAudit(reconStore, auditStore)

	mw := permissions.NewMiddleware(sessStore, userStore, mfaStore, cfgStore)
	g := h.Echo.Group("/api/v1")
	g.Use(mw.RequireAuth)

	g.POST("/reconciliation/statements",       reconHandler.ImportStatements,  mw.RequirePermission("reconciliation:write"))
	g.GET("/reconciliation/statements",        reconHandler.ListImportBatches, mw.RequirePermission("reconciliation:read"))
	g.POST("/reconciliation/runs",             reconHandler.CreateRun,         mw.RequirePermission("reconciliation:write"))
	g.POST("/reconciliation/runs/:id/process", reconHandler.ProcessRun,        mw.RequirePermission("reconciliation:write"))
	g.GET("/reconciliation/runs/:id",          reconHandler.GetRun,            mw.RequirePermission("reconciliation:read"))
	g.GET("/reconciliation/runs/:id/variances",reconHandler.ListVariances,     mw.RequirePermission("reconciliation:read"))

	financeUser := h.MakeUser(ctx, "fiona-finance", "x", "finance")
	readerUser  := h.MakeUser(ctx, "ron-reader",    "x", "approver") // has reconciliation:read but NOT :write

	financeToken := h.SeedSession(ctx, financeUser, true)
	readerToken  := h.SeedSession(ctx, readerUser,  true)

	// Seed a vendor order for the test period (2026-04).
	orderID := "11111111-1111-1111-1111-111111111111"
	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO vendor_orders (id, vendor_name, order_number, order_date, status, total_amount, currency)
		VALUES ($1, 'Acme Corp', 'ORD-TEST', '2026-04-01', 'received', 50000, 'USD')`, orderID)

	// ── 1. Import statements ─────────────────────────────────────────────────
	importBody := map[string]any{
		"source_file": "acme_april_2026.csv",
		"checksum":    "abc123",
		"rows": []map[string]any{
			{
				"order_id":         orderID,
				"line_description": "Acme April invoice",
				"statement_amount": 52000, // overcharge by 2000 cents
				"currency":         "USD",
				"transaction_date": "2026-04-10",
			},
		},
	}

	rec := h.do(t, "POST", "/api/v1/reconciliation/statements", financeToken, importBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// ── 2. Read-only user is forbidden ───────────────────────────────────────
	rec = h.do(t, "POST", "/api/v1/reconciliation/statements", readerToken, importBody)
	if rec.Code != http.StatusForbidden {
		t.Errorf("reader should be forbidden from importing: got %d", rec.Code)
	}

	// But reader CAN list imports.
	rec = h.do(t, "GET", "/api/v1/reconciliation/statements", readerToken, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("reader should be able to list imports: got %d", rec.Code)
	}

	// ── 3. Create + process a run ────────────────────────────────────────────
	rec = h.do(t, "POST", "/api/v1/reconciliation/runs", financeToken,
		map[string]any{"period": "2026-04"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create run: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var run map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &run)
	runID, _ := run["id"].(string)
	if runID == "" {
		t.Fatalf("run has no id: %v", run)
	}

	rec = h.do(t, "POST", "/api/v1/reconciliation/runs/"+runID+"/process", financeToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("process run: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// ── 4. Verify run completed + variances created ──────────────────────────
	rec = h.do(t, "GET", "/api/v1/reconciliation/runs/"+runID, financeToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get run: %d", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &run)
	if status, _ := run["status"].(string); status != "completed" {
		t.Errorf("expected run status=completed, got %q", status)
	}

	rec = h.do(t, "GET", "/api/v1/reconciliation/runs/"+runID+"/variances", financeToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list variances: %d", rec.Code)
	}
	var varResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &varResp)
	variances, _ := varResp["variances"].([]any)
	if len(variances) == 0 {
		t.Error("expected at least one variance from the overcharge")
	}

	// ── 5. Audit row recorded for import ─────────────────────────────────────
	if n := h.AuditCount(ctx, "reconciliation.statements.import", ""); n < 1 {
		t.Errorf("expected at least 1 audit row for statement import, got %d", n)
	}
}

// TestSettlementExportAndAllocation extends the finance flow to verify:
//   1. Settlement batch creation with allocation fields on lines.
//   2. Batch lifecycle: submit → approve → export.
//   3. Export produces CSV content with proper status transition.
//   4. Export job creation via the /exports/jobs endpoint for reconciliation_export type.
//   5. Reader user is forbidden from creating export jobs (no exports:write).
func TestSettlementExportAndAllocation(t *testing.T) {
	h := Setup(t)
	defer h.Cleanup()
	ctx := context.Background()

	auditStore := audit.NewStore(h.Pool)
	userStore  := users.NewStore(h.Pool)
	sessStore  := sessions.NewStore(h.Pool)
	mfaStore   := mfa.NewStore(h.Pool, nil)
	cfgStore   := appconfig.NewStore(h.Pool)
	reconStore := reconciliation.NewStore(h.Pool)
	reconHandler := reconciliation.NewHandlerWithAudit(reconStore, auditStore)

	exportStore   := exports.NewStore(h.Pool)
	exportHandler := exports.NewHandler(exportStore, h.Pool, userStore)

	mw := permissions.NewMiddleware(sessStore, userStore, mfaStore, cfgStore)
	g := h.Echo.Group("/api/v1")
	g.Use(mw.RequireAuth)

	// Reconciliation routes
	g.POST("/reconciliation/statements",                 reconHandler.ImportStatements,  mw.RequirePermission("reconciliation:write"))
	g.POST("/reconciliation/runs",                       reconHandler.CreateRun,         mw.RequirePermission("reconciliation:write"))
	g.POST("/reconciliation/runs/:id/process",           reconHandler.ProcessRun,        mw.RequirePermission("reconciliation:write"))
	g.GET("/reconciliation/runs/:id",                    reconHandler.GetRun,            mw.RequirePermission("reconciliation:read"))
	g.POST("/reconciliation/batches",                    reconHandler.CreateBatch,       mw.RequirePermission("settlements:write"))
	g.POST("/reconciliation/batches/:id/submit",         reconHandler.SubmitBatch,       mw.RequirePermission("settlements:write"))
	g.POST("/reconciliation/batches/:id/approve",        reconHandler.ApproveBatch,      mw.RequirePermission("settlements:write"))
	g.POST("/reconciliation/batches/:id/export",         reconHandler.ExportBatch,       mw.RequirePermission("settlements:write"))

	// Export job routes
	g.POST("/exports/jobs",             exportHandler.CreateJob)
	g.GET("/exports/jobs",              exportHandler.ListJobs)

	financeUser := h.MakeUser(ctx, "settle-finance", "x", "finance")
	readerUser  := h.MakeUser(ctx, "settle-reader",  "x", "approver")

	financeToken := h.SeedSession(ctx, financeUser, true)
	readerToken  := h.SeedSession(ctx, readerUser,  true)

	// Seed a vendor order for the test period.
	orderID := "22222222-2222-2222-2222-222222222222"
	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO vendor_orders (id, vendor_name, order_number, order_date, status, total_amount, currency)
		VALUES ($1, 'Beta Corp', 'ORD-SETTLE', '2026-05-01', 'received', 80000, 'USD')`, orderID)

	// ── 1. Import + create + process a reconciliation run ────────────────────
	importBody := map[string]any{
		"source_file": "beta_may_2026.csv",
		"checksum":    "def456",
		"rows": []map[string]any{
			{
				"order_id":         orderID,
				"line_description": "Beta May invoice",
				"statement_amount": 83000, // overcharge by 3000
				"currency":         "USD",
				"transaction_date": "2026-05-10",
			},
		},
	}
	rec := h.do(t, "POST", "/api/v1/reconciliation/statements", financeToken, importBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import: status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = h.do(t, "POST", "/api/v1/reconciliation/runs", financeToken,
		map[string]any{"period": "2026-05"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create run: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var run map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &run)
	runID, _ := run["id"].(string)

	rec = h.do(t, "POST", "/api/v1/reconciliation/runs/"+runID+"/process", financeToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("process run: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// ── 2. Create settlement batch with allocation fields ────────────────────
	batchBody := map[string]any{
		"run_id": runID,
		"lines": []map[string]any{
			{
				"vendor_order_id": orderID,
				"amount":          3000,
				"direction":       "AP",
				"cost_center_id":  "CC-001",
				"allocations": []map[string]any{
					{
						"department_code": "FIN",
						"cost_center":    "CC-FIN",
						"percentage":     100.0,
					},
				},
			},
		},
	}
	rec = h.do(t, "POST", "/api/v1/reconciliation/batches", financeToken, batchBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create batch: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var batch map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &batch)
	batchID, _ := batch["id"].(string)

	if batch["status"] != "draft" {
		t.Errorf("expected batch status=draft, got %v", batch["status"])
	}

	// ── 3. Batch lifecycle: submit → approve → export ────────────────────────
	rec = h.do(t, "POST", "/api/v1/reconciliation/batches/"+batchID+"/submit", financeToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("submit batch: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var submitted map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &submitted)
	if submitted["status"] != "under_review" {
		t.Errorf("expected status=under_review after submit, got %v", submitted["status"])
	}

	rec = h.do(t, "POST", "/api/v1/reconciliation/batches/"+batchID+"/approve", financeToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve batch: status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = h.do(t, "POST", "/api/v1/reconciliation/batches/"+batchID+"/export", financeToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export batch: status=%d body=%s", rec.Code, rec.Body.String())
	}
	// Export should return CSV content.
	contentType := rec.Header().Get("Content-Type")
	if contentType == "" || (contentType != "text/csv; charset=utf-8" && contentType != "text/csv") {
		t.Errorf("expected CSV content-type, got %q", contentType)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("expected non-empty CSV body from export")
	}

	// ── 3b. Parse CSV and verify allocation columns/values ───────────────────
	csvReader := csv.NewReader(strings.NewReader(rec.Body.String()))
	csvRows, err := csvReader.ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(csvRows) < 2 {
		t.Fatalf("expected header + at least 1 data row, got %d rows", len(csvRows))
	}

	headers := csvRows[0]
	// Build column index map.
	colIdx := make(map[string]int)
	for i, h := range headers {
		colIdx[h] = i
	}

	// Verify allocation-specific columns exist.
	for _, requiredCol := range []string{"alloc_department_code", "alloc_cost_center", "alloc_amount", "alloc_pct"} {
		if _, ok := colIdx[requiredCol]; !ok {
			t.Errorf("CSV missing required allocation column %q; headers: %v", requiredCol, headers)
		}
	}

	// Verify the first data row contains the allocation values we supplied.
	dataRow := csvRows[1]
	if idx, ok := colIdx["alloc_department_code"]; ok && dataRow[idx] != "FIN" {
		t.Errorf("expected alloc_department_code=FIN, got %q", dataRow[idx])
	}
	if idx, ok := colIdx["alloc_cost_center"]; ok && dataRow[idx] != "CC-FIN" {
		t.Errorf("expected alloc_cost_center=CC-FIN, got %q", dataRow[idx])
	}
	if idx, ok := colIdx["direction"]; ok && dataRow[idx] != "AP" {
		t.Errorf("expected direction=AP, got %q", dataRow[idx])
	}
	if idx, ok := colIdx["cost_center_id"]; ok && dataRow[idx] != "CC-001" {
		t.Errorf("expected cost_center_id=CC-001, got %q", dataRow[idx])
	}

	// ── 4. Export job creation for reconciliation_export ──────────────────────
	rec = h.do(t, "POST", "/api/v1/exports/jobs", financeToken,
		map[string]any{"type": "reconciliation_export"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create export job: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var exportJob map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &exportJob)
	if exportJob["type"] != "reconciliation_export" {
		t.Errorf("expected type=reconciliation_export, got %v", exportJob["type"])
	}
	if exportJob["status"] != "queued" {
		t.Errorf("expected status=queued, got %v", exportJob["status"])
	}

	// ── 5. Reader user cannot create reconciliation export jobs ──────────────
	rec = h.do(t, "POST", "/api/v1/exports/jobs", readerToken,
		map[string]any{"type": "reconciliation_export"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("reader should be forbidden from creating recon export, got %d", rec.Code)
	}

	// ── 6. Finance user's export jobs are listed ─────────────────────────────
	rec = h.do(t, "GET", "/api/v1/exports/jobs", financeToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list export jobs: status=%d", rec.Code)
	}
	var jobsList map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &jobsList)
	jobs, _ := jobsList["jobs"].([]any)
	if len(jobs) == 0 {
		t.Error("expected at least one export job in list")
	}

	// ── 7. Audit row recorded for batch export ───────────────────────────────
	if n := h.AuditCount(ctx, "settlement.batch.export", batchID); n < 1 {
		t.Errorf("expected at least 1 audit row for batch export, got %d", n)
	}
}

// TestReconciliationExportCSVContent verifies the generated CSV content of a
// reconciliation export after the initiated_by fix. Covers three cases:
//   - Positive: user A's scoped export includes A's API-created run
//   - Negative: user A's scoped export excludes user B's run
//   - Admin:    unscoped export includes both runs
//
// Uses WriteReconciliationCSVForTest to generate CSV into a buffer and parse it
// directly, exercising the real SQL against a live database.
func TestReconciliationExportCSVContent(t *testing.T) {
	h := Setup(t)
	defer h.Cleanup()
	ctx := context.Background()

	auditStore := audit.NewStore(h.Pool)
	userStore  := users.NewStore(h.Pool)
	sessStore  := sessions.NewStore(h.Pool)
	mfaStore   := mfa.NewStore(h.Pool, nil)
	cfgStore   := appconfig.NewStore(h.Pool)
	reconStore := reconciliation.NewStore(h.Pool)
	reconHandler := reconciliation.NewHandlerWithAudit(reconStore, auditStore)

	mw := permissions.NewMiddleware(sessStore, userStore, mfaStore, cfgStore)
	g := h.Echo.Group("/api/v1")
	g.Use(mw.RequireAuth)

	g.POST("/reconciliation/statements",       reconHandler.ImportStatements,  mw.RequirePermission("reconciliation:write"))
	g.POST("/reconciliation/runs",             reconHandler.CreateRun,         mw.RequirePermission("reconciliation:write"))
	g.POST("/reconciliation/runs/:id/process", reconHandler.ProcessRun,        mw.RequirePermission("reconciliation:write"))

	// Two finance users, each will create a run.
	userA := h.MakeUser(ctx, "csv-finance-a", "pw", "finance")
	userB := h.MakeUser(ctx, "csv-finance-b", "pw", "finance")
	tokenA := h.SeedSession(ctx, userA, true)
	tokenB := h.SeedSession(ctx, userB, true)

	// Seed vendor orders for two separate periods so the runs don't collide.
	orderA := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	orderB := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO vendor_orders (id, vendor_name, order_number, order_date, status, total_amount, currency)
		VALUES ($1, 'Vendor A', 'ORD-A', '2026-08-01', 'received', 10000, 'USD')`, orderA)
	_, _ = h.Pool.Exec(ctx, `
		INSERT INTO vendor_orders (id, vendor_name, order_number, order_date, status, total_amount, currency)
		VALUES ($1, 'Vendor B', 'ORD-B', '2026-09-01', 'received', 20000, 'USD')`, orderB)

	// User A: import statement + create + process run for 2026-08.
	rec := h.do(t, "POST", "/api/v1/reconciliation/statements", tokenA, map[string]any{
		"source_file": "a.csv", "checksum": "a1",
		"rows": []map[string]any{{
			"order_id": orderA, "line_description": "A invoice",
			"statement_amount": 12000, "currency": "USD", "transaction_date": "2026-08-10",
		}},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import A: %d %s", rec.Code, rec.Body.String())
	}
	rec = h.do(t, "POST", "/api/v1/reconciliation/runs", tokenA, map[string]any{"period": "2026-08"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create run A: %d %s", rec.Code, rec.Body.String())
	}
	var runAResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &runAResp)
	runAID, _ := runAResp["id"].(string)
	rec = h.do(t, "POST", "/api/v1/reconciliation/runs/"+runAID+"/process", tokenA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("process run A: %d %s", rec.Code, rec.Body.String())
	}

	// User B: import statement + create + process run for 2026-09.
	rec = h.do(t, "POST", "/api/v1/reconciliation/statements", tokenB, map[string]any{
		"source_file": "b.csv", "checksum": "b1",
		"rows": []map[string]any{{
			"order_id": orderB, "line_description": "B invoice",
			"statement_amount": 25000, "currency": "USD", "transaction_date": "2026-09-10",
		}},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import B: %d %s", rec.Code, rec.Body.String())
	}
	rec = h.do(t, "POST", "/api/v1/reconciliation/runs", tokenB, map[string]any{"period": "2026-09"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create run B: %d %s", rec.Code, rec.Body.String())
	}
	var runBResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &runBResp)
	runBID, _ := runBResp["id"].(string)
	rec = h.do(t, "POST", "/api/v1/reconciliation/runs/"+runBID+"/process", tokenB, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("process run B: %d %s", rec.Code, rec.Body.String())
	}

	// Confirm both runs were stored with initiated_by (not run_by).
	for _, tc := range []struct{ runID, userID, label string }{
		{runAID, userA, "A"}, {runBID, userB, "B"},
	} {
		var ib *string
		_ = h.Pool.QueryRow(ctx,
			`SELECT initiated_by::TEXT FROM reconciliation_runs WHERE id = $1`, tc.runID).Scan(&ib)
		if ib == nil || *ib != tc.userID {
			t.Fatalf("run %s: expected initiated_by=%s, got %v", tc.label, tc.userID, ib)
		}
	}

	// Helper: generate CSV into buffer and parse rows + column index.
	parseCSV := func(t *testing.T, scopedBy string) ([][]string, map[string]int) {
		t.Helper()
		var buf bytes.Buffer
		if err := exports.WriteReconciliationCSVForTest(ctx, h.Pool, &buf, scopedBy); err != nil {
			t.Fatalf("WriteReconciliationCSVForTest(scope=%q): %v", scopedBy, err)
		}
		reader := csv.NewReader(strings.NewReader(buf.String()))
		rows, err := reader.ReadAll()
		if err != nil {
			t.Fatalf("parse CSV: %v", err)
		}
		if len(rows) == 0 {
			t.Fatal("CSV is empty")
		}
		idx := make(map[string]int)
		for i, h := range rows[0] {
			idx[h] = i
		}
		return rows, idx
	}

	// Helper: collect run_id values from CSV data rows.
	runIDs := func(rows [][]string, idx map[string]int) map[string]bool {
		col := idx["run_id"]
		out := map[string]bool{}
		for _, r := range rows[1:] {
			out[r[col]] = true
		}
		return out
	}

	// ── Case 1: Scoped to user A — must include A's run, must exclude B's ────
	t.Run("scoped_to_user_A", func(t *testing.T) {
		rows, idx := parseCSV(t, userA)

		// Header check.
		if _, ok := idx["initiated_by"]; !ok {
			t.Errorf("header missing 'initiated_by'; got %v", rows[0])
		}
		if _, ok := idx["run_by"]; ok {
			t.Errorf("header must not contain legacy 'run_by'; got %v", rows[0])
		}

		ids := runIDs(rows, idx)
		if !ids[runAID] {
			t.Errorf("user A's run %s must appear in A-scoped export; run_ids: %v", runAID, ids)
		}
		if ids[runBID] {
			t.Errorf("user B's run %s must NOT appear in A-scoped export; run_ids: %v", runBID, ids)
		}

		// Verify the initiated_by column value for A's run.
		ibCol := idx["initiated_by"]
		for _, r := range rows[1:] {
			if r[idx["run_id"]] == runAID {
				if r[ibCol] != userA {
					t.Errorf("expected initiated_by=%s for run A, got %q", userA, r[ibCol])
				}
			}
		}
	})

	// ── Case 2: Scoped to user B — must include B's run, must exclude A's ────
	t.Run("scoped_to_user_B", func(t *testing.T) {
		rows, idx := parseCSV(t, userB)

		ids := runIDs(rows, idx)
		if !ids[runBID] {
			t.Errorf("user B's run %s must appear in B-scoped export; run_ids: %v", runBID, ids)
		}
		if ids[runAID] {
			t.Errorf("user A's run %s must NOT appear in B-scoped export; run_ids: %v", runAID, ids)
		}
	})

	// ── Case 3: Unscoped (admin) — must include both runs ────────────────────
	t.Run("unscoped_admin", func(t *testing.T) {
		rows, idx := parseCSV(t, "")

		ids := runIDs(rows, idx)
		if !ids[runAID] {
			t.Errorf("run A %s must appear in unscoped export; run_ids: %v", runAID, ids)
		}
		if !ids[runBID] {
			t.Errorf("run B %s must appear in unscoped export; run_ids: %v", runBID, ids)
		}
	})
}

// TestUnmatchedStatementRowCreatesVariance verifies that statement rows
// without a matching vendor order produce an "unexpected_statement" variance.
func TestUnmatchedStatementRowCreatesVariance(t *testing.T) {
	h := Setup(t)
	defer h.Cleanup()
	ctx := context.Background()

	auditStore := audit.NewStore(h.Pool)
	userStore  := users.NewStore(h.Pool)
	sessStore  := sessions.NewStore(h.Pool)
	mfaStore   := mfa.NewStore(h.Pool, nil)
	cfgStore   := appconfig.NewStore(h.Pool)
	reconStore := reconciliation.NewStore(h.Pool)
	reconHandler := reconciliation.NewHandlerWithAudit(reconStore, auditStore)

	mw := permissions.NewMiddleware(sessStore, userStore, mfaStore, cfgStore)
	g := h.Echo.Group("/api/v1")
	g.Use(mw.RequireAuth)

	g.POST("/reconciliation/statements",       reconHandler.ImportStatements,  mw.RequirePermission("reconciliation:write"))
	g.POST("/reconciliation/runs",             reconHandler.CreateRun,         mw.RequirePermission("reconciliation:write"))
	g.POST("/reconciliation/runs/:id/process", reconHandler.ProcessRun,        mw.RequirePermission("reconciliation:write"))
	g.GET("/reconciliation/runs/:id/variances",reconHandler.ListVariances,     mw.RequirePermission("reconciliation:read"))

	financeUser := h.MakeUser(ctx, "unmatched-finance", "pw", "finance")
	financeToken := h.SeedSession(ctx, financeUser, true)

	// Import a statement row with NO order_id — it has no matching vendor order.
	rec := h.do(t, "POST", "/api/v1/reconciliation/statements", financeToken, map[string]any{
		"source_file": "orphan_statement.csv",
		"checksum":    "orphan123",
		"rows": []map[string]any{{
			"order_id":         "", // no matching order
			"line_description": "Mystery charge from unknown vendor",
			"statement_amount": 7500,
			"currency":         "USD",
			"transaction_date": "2026-07-05",
		}},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Create and process a run for the same period.
	rec = h.do(t, "POST", "/api/v1/reconciliation/runs", financeToken,
		map[string]any{"period": "2026-07"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create run: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var run map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &run)
	runID, _ := run["id"].(string)

	rec = h.do(t, "POST", "/api/v1/reconciliation/runs/"+runID+"/process", financeToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("process run: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Fetch variances and verify the unmatched statement row created one.
	rec = h.do(t, "GET", "/api/v1/reconciliation/runs/"+runID+"/variances", financeToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list variances: status=%d", rec.Code)
	}
	var varResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &varResp)
	variances, _ := varResp["variances"].([]any)

	foundUnexpected := false
	for _, v := range variances {
		vm, _ := v.(map[string]any)
		if vm["variance_type"] == "unexpected_statement" {
			foundUnexpected = true
			if vm["expected_amount"] != 0.0 {
				t.Errorf("expected expected_amount=0 for unexpected_statement, got %v", vm["expected_amount"])
			}
			if vm["actual_amount"] != 7500.0 {
				t.Errorf("expected actual_amount=7500 for unexpected_statement, got %v", vm["actual_amount"])
			}
			suggestion, _ := vm["suggestion"].(string)
			if suggestion == "" {
				t.Error("expected non-empty suggestion for unexpected_statement variance")
			}
			break
		}
	}
	if !foundUnexpected {
		t.Errorf("expected at least one 'unexpected_statement' variance for unmatched statement row; variances: %v", variances)
	}
}

// do issues a request against h.Echo with the session cookie set.
func (h *Harness) doFinance(t *testing.T, method, path, token string, body any) *bytes.Buffer {
	t.Helper()
	rec := h.do(t, method, path, token, body)
	return rec.Body
}
