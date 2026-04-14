// Package reconciliation manages billing rules, reconciliation runs, variances,
// and settlement batches for the finance workflow.
package reconciliation

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ─────────────────────────────────────────────────────────────────────────────
// Domain types
// ─────────────────────────────────────────────────────────────────────────────

// BillingRule represents a version of a billing rule set.
type BillingRule struct {
	ID            int64           `json:"id"`
	RuleSetID     int64           `json:"rule_set_id"`
	RuleSetName   string          `json:"rule_set_name"`
	VersionNumber int             `json:"version_number"`
	Description   string          `json:"description"`
	EffectiveFrom string          `json:"effective_from"`
	EffectiveTo   *string         `json:"effective_to,omitempty"`
	Rules         json.RawMessage `json:"rules"`
	CreatedAt     time.Time       `json:"created_at"`
}

// ReconciliationRun is an API-driven reconciliation run for a billing period.
type ReconciliationRun struct {
	ID          string          `json:"id"`
	Period      string          `json:"period"`
	Status      string          `json:"status"`
	InitiatedBy string          `json:"initiated_by"`
	CreatedAt   time.Time       `json:"created_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	SummaryJSON json.RawMessage `json:"summary,omitempty"`
}

// Variance is a detected amount difference for a vendor order in a run.
type Variance struct {
	ID             string  `json:"id"`
	RunID          string  `json:"run_id"`
	VendorOrderID  string  `json:"vendor_order_id"`
	ExpectedAmount int64   `json:"expected_amount"`
	ActualAmount   int64   `json:"actual_amount"`
	Delta          int64   `json:"delta"`
	VarianceType   string  `json:"variance_type"`
	Suggestion     string  `json:"suggestion,omitempty"`
	Status         string  `json:"status"`
}

// SettlementBatch groups settlement lines for a reconciliation run.
type SettlementBatch struct {
	ID                string           `json:"id"`
	RunID             string           `json:"run_id"`
	Status            string           `json:"status"`
	CreatedBy         string           `json:"created_by"`
	FinanceApprovedBy *string          `json:"finance_approved_by,omitempty"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
	ApprovedAt        *time.Time       `json:"approved_at,omitempty"`
	ExportedAt        *time.Time       `json:"exported_at,omitempty"`
	Lines             []SettlementLine `json:"lines,omitempty"`
}

// SettlementLine is one line item in a settlement batch.
type SettlementLine struct {
	ID            string           `json:"id"`
	BatchID       string           `json:"batch_id"`
	VendorOrderID *string          `json:"vendor_order_id,omitempty"`
	Amount        int64            `json:"amount"`
	Direction     string           `json:"direction"` // AR or AP
	CostCenterID  string           `json:"cost_center_id,omitempty"`
	Allocations   []CostAllocation `json:"allocations,omitempty"`
}

// CostAllocation splits a settlement line across departments and cost centers.
// Department and cost-center are separate fields per the schema; the API
// carries both so downstream GL systems can map to distinct dimensions.
type CostAllocation struct {
	ID             int64    `json:"id"`
	LineID         string   `json:"line_id"`
	DepartmentCode string   `json:"department_code"`
	CostCenter     string   `json:"cost_center"`
	Amount         int64    `json:"amount"`
	Percentage     *float64 `json:"percentage,omitempty"`
}

// SettlementLineInput is used when creating a batch.
type SettlementLineInput struct {
	VendorOrderID *string           `json:"vendor_order_id"`
	Amount        int64             `json:"amount"`
	Direction     string            `json:"direction"`
	CostCenterID  string            `json:"cost_center_id"`
	Allocations   []AllocationInput `json:"allocations,omitempty"`
}

// AllocationInput specifies a cost allocation on line creation.
type AllocationInput struct {
	DepartmentCode string   `json:"department_code"`
	CostCenter     string   `json:"cost_center"`
	Amount         int64    `json:"amount,omitempty"`
	Percentage     *float64 `json:"percentage,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Store
// ─────────────────────────────────────────────────────────────────────────────

// WebhookEmitter is the minimal interface a Store needs to fan out
// settlement-batch state events to LAN webhook subscribers. Implemented by
// internal/app/webhooks.Store.Deliver — abstracted to avoid an import cycle.
type WebhookEmitter interface {
	Deliver(ctx context.Context, eventType string, payload map[string]any) error
}

// Store handles reconciliation persistence.
type Store struct {
	pool     *pgxpool.Pool
	webhooks WebhookEmitter // optional; settlement state transitions fan out to subscribers
}

// NewStore constructs a Store backed by the given connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// WithWebhooks returns a copy of s with the webhook emitter wired so settlement
// state transitions (approved → exported → settled) emit subscription events.
func (s *Store) WithWebhooks(emitter WebhookEmitter) *Store {
	cp := *s
	cp.webhooks = emitter
	return &cp
}

// emitWebhook wraps Deliver in a nil check so callers can fire-and-forget
// without worrying about whether webhooks were wired.
func (s *Store) emitWebhook(ctx context.Context, eventType string, payload map[string]any) {
	if s.webhooks == nil {
		return
	}
	_ = s.webhooks.Deliver(ctx, eventType, payload)
}

// ─────────────────────────────────────────────────────────────────────────────
// Billing rules
// ─────────────────────────────────────────────────────────────────────────────

// ListBillingRules returns all billing rule versions with their rule set names.
func (s *Store) ListBillingRules(ctx context.Context) ([]BillingRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			brv.id,
			brv.rule_set_id,
			brs.name          AS rule_set_name,
			brv.version_number,
			COALESCE(brs.description, '') AS description,
			brv.effective_from::TEXT,
			brv.effective_to::TEXT,
			brv.rule_definition,
			brv.created_at
		FROM billing_rule_versions brv
		JOIN billing_rule_sets brs ON brs.id = brv.rule_set_id
		ORDER BY brv.rule_set_id, brv.version_number DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list billing rules: %w", err)
	}
	defer rows.Close()

	var out []BillingRule
	for rows.Next() {
		var r BillingRule
		var effTo *string
		if err := rows.Scan(
			&r.ID, &r.RuleSetID, &r.RuleSetName,
			&r.VersionNumber, &r.Description,
			&r.EffectiveFrom, &effTo,
			&r.Rules, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		r.EffectiveTo = effTo
		out = append(out, r)
	}
	if out == nil {
		out = []BillingRule{}
	}
	return out, nil
}

// GetBillingRule returns a single billing rule version by ID.
func (s *Store) GetBillingRule(ctx context.Context, id string) (*BillingRule, error) {
	r := &BillingRule{}
	var effTo *string
	err := s.pool.QueryRow(ctx, `
		SELECT
			brv.id, brv.rule_set_id, brs.name,
			brv.version_number, COALESCE(brs.description, ''),
			brv.effective_from::TEXT, brv.effective_to::TEXT,
			brv.rule_definition, brv.created_at
		FROM billing_rule_versions brv
		JOIN billing_rule_sets brs ON brs.id = brv.rule_set_id
		WHERE brv.id = $1`, id).
		Scan(&r.ID, &r.RuleSetID, &r.RuleSetName,
			&r.VersionNumber, &r.Description,
			&r.EffectiveFrom, &effTo,
			&r.Rules, &r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get billing rule: %w", err)
	}
	r.EffectiveTo = effTo
	return r, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Reconciliation runs
// ─────────────────────────────────────────────────────────────────────────────

// CreateReconciliationRun inserts a new run with status='pending'.
// All runs now live in reconciliation_runs; period and initiated_by are stored
// in the columns added by migration 007.
func (s *Store) CreateReconciliationRun(ctx context.Context, period, initiatedBy string) (*ReconciliationRun, error) {
	id := uuid.New().String()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO reconciliation_runs
			(id, period, status, initiated_by, started_at)
		VALUES ($1, $2, 'pending', $3, NOW())`,
		id, period, initiatedBy)
	if err != nil {
		return nil, fmt.Errorf("create reconciliation run: %w", err)
	}
	return s.GetRun(ctx, id)
}

// ListRuns returns paginated runs, newest first, plus total count.
func (s *Store) ListRuns(ctx context.Context, limit, offset int) ([]ReconciliationRun, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var total int
	_ = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM reconciliation_runs WHERE period IS NOT NULL`).Scan(&total)

	rows, err := s.pool.Query(ctx, `
		SELECT id, period, status,
		       COALESCE(initiated_by::TEXT, run_by::TEXT, ''),
		       started_at, completed_at, summary_json
		FROM reconciliation_runs
		WHERE period IS NOT NULL
		ORDER BY started_at DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	var out []ReconciliationRun
	for rows.Next() {
		var r ReconciliationRun
		if err := rows.Scan(&r.ID, &r.Period, &r.Status, &r.InitiatedBy,
			&r.CreatedAt, &r.CompletedAt, &r.SummaryJSON); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	if out == nil {
		out = []ReconciliationRun{}
	}
	return out, total, nil
}

// GetRun fetches a single reconciliation run by ID.
func (s *Store) GetRun(ctx context.Context, runID string) (*ReconciliationRun, error) {
	r := &ReconciliationRun{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, COALESCE(period, ''), status,
		       COALESCE(initiated_by::TEXT, run_by::TEXT, ''),
		       started_at, completed_at, summary_json
		FROM reconciliation_runs WHERE id = $1`, runID).
		Scan(&r.ID, &r.Period, &r.Status, &r.InitiatedBy,
			&r.CreatedAt, &r.CompletedAt, &r.SummaryJSON)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	return r, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Statement imports
// ─────────────────────────────────────────────────────────────────────────────

// StatementImportBatch mirrors the statement_import_batches table.
type StatementImportBatch struct {
	ID         string    `json:"id"`
	ImportedBy string    `json:"imported_by"`
	SourceFile string    `json:"source_file"`
	Checksum   string    `json:"checksum"`
	RowCount   int       `json:"row_count"`
	Status     string    `json:"status"`
	ImportedAt time.Time `json:"imported_at"`
}

// StatementRowInput is the shape of one row in an uploaded statement CSV.
type StatementRowInput struct {
	OrderID         string `json:"order_id"`         // UUID of matched vendor order (optional)
	LineDescription string `json:"line_description"`
	StatementAmount int64  `json:"statement_amount"` // minor units
	Currency        string `json:"currency"`
	TransactionDate string `json:"transaction_date"` // YYYY-MM-DD
}

// ImportStatements creates a statement_import_batches row and inserts the
// accompanying statement_rows. Returns the batch record. This is the API
// entry point that finance operators use to upload vendor statement data
// before running reconciliation.
func (s *Store) ImportStatements(ctx context.Context, importerID, sourceFile, checksum string, rows []StatementRowInput) (*StatementImportBatch, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("at least one statement row is required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin import tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	batchID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO statement_import_batches (id, imported_by, source_file, checksum, row_count, status)
		VALUES ($1, $2::uuid, $3, $4, $5, 'processed')`,
		batchID, importerID, sourceFile, checksum, len(rows))
	if err != nil {
		return nil, fmt.Errorf("insert batch: %w", err)
	}

	for _, r := range rows {
		cur := r.Currency
		if cur == "" {
			cur = "USD"
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO statement_rows (batch_id, order_id, line_description, statement_amount, currency, transaction_date, raw_data)
			VALUES ($1, NULLIF($2,'')::uuid, $3, $4, $5, $6::date, NULL)`,
			batchID, r.OrderID, r.LineDescription, r.StatementAmount, cur, r.TransactionDate)
		if err != nil {
			return nil, fmt.Errorf("insert row: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit import: %w", err)
	}

	return &StatementImportBatch{
		ID: batchID, ImportedBy: importerID, SourceFile: sourceFile,
		Checksum: checksum, RowCount: len(rows), Status: "processed",
		ImportedAt: time.Now(),
	}, nil
}

// ListImportBatches returns the most recent statement import batches.
func (s *Store) ListImportBatches(ctx context.Context, limit int) ([]StatementImportBatch, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, imported_by::text, source_file, checksum, row_count, status, imported_at
		FROM statement_import_batches
		ORDER BY imported_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StatementImportBatch
	for rows.Next() {
		var b StatementImportBatch
		if err := rows.Scan(&b.ID, &b.ImportedBy, &b.SourceFile, &b.Checksum,
			&b.RowCount, &b.Status, &b.ImportedAt); err != nil {
			continue
		}
		out = append(out, b)
	}
	if out == nil {
		out = []StatementImportBatch{}
	}
	return out, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// ProcessRun — real statement comparison
// ─────────────────────────────────────────────────────────────────────────────

// ProcessRun compares vendor_orders against statement_rows for the run's period.
// A real statement import batch must exist for the period; if none is found the
// run is failed with a descriptive error instead of auto-generating synthetic data.
func (s *Store) ProcessRun(ctx context.Context, runID string) error {
	run, err := s.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if run.Status != "pending" {
		return fmt.Errorf("run is not in pending state (current: %s)", run.Status)
	}

	// Require a real import batch for the period — no synthetic fallback.
	var batchID string
	err = s.pool.QueryRow(ctx, `
		SELECT DISTINCT sr.batch_id::TEXT
		FROM statement_rows sr
		JOIN statement_import_batches sib ON sib.id = sr.batch_id
		WHERE TO_CHAR(sr.transaction_date, 'YYYY-MM') = $1
		LIMIT 1`, run.Period).Scan(&batchID)
	if err != nil || batchID == "" {
		_, _ = s.pool.Exec(ctx,
			`UPDATE reconciliation_runs SET status = 'failed' WHERE id = $1`, runID)
		return fmt.Errorf("no statement import batch found for period %s: import vendor statements before processing", run.Period)
	}

	// Mark as processing and attach the real batch.
	_, err = s.pool.Exec(ctx,
		`UPDATE reconciliation_runs SET status = 'processing', statement_import_batch_id = $1 WHERE id = $2`,
		batchID, runID)
	if err != nil {
		return fmt.Errorf("mark processing: %w", err)
	}

	// Query vendor orders for the period joined against statement_rows.
	// We use a LEFT JOIN so orders without a statement row also appear (delta = 0 - total_amount).
	// statement_rows.id is BIGSERIAL; scan sr.id as *int64 so NULL is preserved
	// for missing-statement cases (reconciliation_variances.statement_row_id is BIGINT).
	type orderComparison struct {
		orderID         string
		totalAmount     int64
		statementAmount int64
		hasStatement    bool
		statementRowID  *int64 // NULL for missing-statement cases
	}

	rows, err := s.pool.Query(ctx, `
		SELECT
			vo.id,
			vo.total_amount,
			COALESCE(sr.statement_amount, 0) AS statement_amount,
			(sr.id IS NOT NULL)              AS has_statement,
			sr.id                            AS statement_row_id
		FROM vendor_orders vo
		LEFT JOIN statement_rows sr
			ON sr.vendor_order_id = vo.id
			AND sr.batch_id = $2
		WHERE TO_CHAR(vo.order_date, 'YYYY-MM') = $1
		   OR TO_CHAR(vo.created_at, 'YYYY-MM') = $1
		LIMIT 500`, run.Period, batchID)
	if err != nil {
		return fmt.Errorf("query orders for comparison: %w", err)
	}
	defer rows.Close()

	var comparisons []orderComparison
	for rows.Next() {
		var oc orderComparison
		if err := rows.Scan(&oc.orderID, &oc.totalAmount,
			&oc.statementAmount, &oc.hasStatement, &oc.statementRowID); err != nil {
			return err
		}
		comparisons = append(comparisons, oc)
	}
	rows.Close()

	// Detect unmatched statement rows — statement rows that have no matching
	// vendor order. These are surfaced as "unexpected_statement" variances.
	type unmatchedStatement struct {
		statementRowID  int64
		statementAmount int64
		lineDescription string
	}

	unmatchedRows, err := s.pool.Query(ctx, `
		SELECT sr.id, sr.statement_amount, COALESCE(sr.line_description, '')
		FROM statement_rows sr
		WHERE sr.batch_id = $1
		  AND sr.vendor_order_id IS NULL
		  AND sr.order_id IS NULL
		LIMIT 500`, batchID)
	if err != nil {
		return fmt.Errorf("query unmatched statement rows: %w", err)
	}
	defer unmatchedRows.Close()

	var unmatchedStatements []unmatchedStatement
	for unmatchedRows.Next() {
		var us unmatchedStatement
		if err := unmatchedRows.Scan(&us.statementRowID, &us.statementAmount, &us.lineDescription); err != nil {
			return err
		}
		unmatchedStatements = append(unmatchedStatements, us)
	}
	unmatchedRows.Close()

	varianceCount := 0
	totalDelta := int64(0)

	for _, oc := range comparisons {
		expected := oc.totalAmount
		actual := oc.statementAmount

		// Determine variance type.
		var varType string
		if !oc.hasStatement {
			varType = "missing_statement"
		} else if actual > expected {
			varType = "overcharge"
		} else {
			varType = "undercharge"
		}

		delta := actual - expected

		// Skip if within 2% threshold (only for orders that have a statement).
		if oc.hasStatement && expected != 0 {
			pctDiff := math.Abs(float64(delta)) / float64(expected)
			if pctDiff <= 0.02 {
				continue
			}
		}

		// Skip zero-amount orders without statements (nothing meaningful to record).
		if !oc.hasStatement && expected == 0 {
			continue
		}

		varianceCount++
		totalDelta += delta

		varID := uuid.New().String()
		suggestion := buildVarianceSuggestion(varType, delta, expected)

		// Configurable write-off threshold: when the absolute variance is at or
		// below the threshold (seeded as reconciliation.writeoff_threshold_cents
		// in config_parameters, defaulting to 500 = $5.00) the suggestion
		// includes an auto-suggest note and the initial status is set to 'open'
		// (Finance still must approve). threshold_used is persisted so auditors
		// can trace which threshold was applied at processing time.
		absDelta := delta
		if absDelta < 0 {
			absDelta = -absDelta
		}
		var thresholdUsed *int64
		writeoffThreshold := int64(500) // $5.00 in minor units — default
		var threshCfg string
		if err := s.pool.QueryRow(ctx,
			`SELECT param_value FROM config_parameters WHERE param_key = 'writeoff.auto_suggest_threshold'`,
		).Scan(&threshCfg); err == nil {
			if parsed, convErr := strconv.ParseInt(threshCfg, 10, 64); convErr == nil && parsed > 0 {
				writeoffThreshold = parsed
			}
		}
		if absDelta > 0 && absDelta <= writeoffThreshold {
			suggestion += fmt.Sprintf(" [Auto-suggest: under threshold $%.2f — recommend write-off, pending Finance approval.]",
				float64(writeoffThreshold)/100)
			thresholdUsed = &writeoffThreshold
		}

		// Insert into reconciliation_variances.
		// statement_row_id is the real statement_rows.id (BIGINT) when present;
		// NULL only for missing-statement cases (column is nullable per migration 008).
		_, insertErr := s.pool.Exec(ctx, `
			INSERT INTO reconciliation_variances
				(id, run_id, statement_row_id, order_id,
				 expected_amount, actual_amount, variance_amount,
				 variance_type, suggestion, delta, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'open')
			ON CONFLICT (id) DO NOTHING`,
			varID, runID, oc.statementRowID, oc.orderID,
			expected, actual, delta,
			varType, suggestion, delta)
		// Persist threshold_used on the corresponding writeoff_suggestion row
		// (schema: variance_writeoff_suggestions.threshold_used) if the threshold
		// triggered auto-suggest.
		if insertErr == nil && thresholdUsed != nil {
			_, _ = s.pool.Exec(ctx, `
				INSERT INTO variance_writeoff_suggestions
					(id, variance_id, suggestion_reason, threshold_used, status)
				VALUES (gen_random_uuid(), $1, $2, $3, 'pending')`,
				varID, suggestion, *thresholdUsed)
		}
		if insertErr != nil {
			// Non-fatal — continue processing other orders.
			continue
		}
	}

	// Process unmatched statement rows (statement with no order).
	for _, us := range unmatchedStatements {
		varianceCount++
		delta := us.statementAmount // no expected amount — entire amount is unexpected
		totalDelta += delta

		varID := uuid.New().String()
		suggestion := fmt.Sprintf("Statement row has no matching vendor order (%s, %d minor units). Investigate unexpected charge.",
			us.lineDescription, us.statementAmount)

		stmtRowID := us.statementRowID
		_, insertErr := s.pool.Exec(ctx, `
			INSERT INTO reconciliation_variances
				(id, run_id, statement_row_id, order_id,
				 expected_amount, actual_amount, variance_amount,
				 variance_type, suggestion, delta, status)
			VALUES ($1, $2, $3, NULL, 0, $4, $4, 'unexpected_statement', $5, $4, 'open')
			ON CONFLICT (id) DO NOTHING`,
			varID, runID, &stmtRowID, us.statementAmount, suggestion)
		if insertErr != nil {
			continue
		}
	}

	// Build summary JSON.
	summaryMap := map[string]any{
		"orders_checked":  len(comparisons),
		"variances_found": varianceCount,
		"total_delta":     totalDelta,
		"processed_at":    time.Now().UTC().Format(time.RFC3339),
	}
	summaryBytes, _ := json.Marshal(summaryMap)
	now := time.Now()

	_, err = s.pool.Exec(ctx, `
		UPDATE reconciliation_runs
		SET status = 'completed', completed_at = $1, summary_json = $2,
		    total_variances = $3, total_variance_amount = $4
		WHERE id = $5`, now, summaryBytes, varianceCount, totalDelta, runID)
	if err != nil {
		return fmt.Errorf("finalize run: %w", err)
	}

	return nil
}


func buildVarianceSuggestion(varType string, delta, expected int64) string {
	switch varType {
	case "missing_statement":
		return fmt.Sprintf("No statement row found for this order (expected %d minor units). Investigate missing vendor statement.", expected)
	case "unexpected_statement":
		return fmt.Sprintf("Statement row has no matching vendor order (%d minor units). Investigate unexpected charge.", delta)
	case "overcharge":
		pct := float64(delta) / float64(expected) * 100
		return fmt.Sprintf("Write-off $%.2f — Overcharge of %d minor units (%.1f%%). Recommend requesting credit note.", float64(delta)/100, delta, pct)
	default: // undercharge
		pct := float64(-delta) / float64(expected) * 100
		return fmt.Sprintf("Write-off $%.2f — Underpayment of %d minor units (%.1f%%). Recommend issuing supplemental payment.", float64(-delta)/100, -delta, pct)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Variances
// ─────────────────────────────────────────────────────────────────────────────

// ListVariances returns variances for a run, optionally filtered by status.
func (s *Store) ListVariances(ctx context.Context, runID, status string) ([]Variance, error) {
	query := `
		SELECT id, run_id, COALESCE(order_id::TEXT, ''),
		       expected_amount, actual_amount, COALESCE(delta, variance_amount),
		       COALESCE(variance_type, 'amount'),
		       COALESCE(suggestion, ''), status
		FROM reconciliation_variances
		WHERE run_id = $1`
	args := []any{runID}

	if status != "" {
		query += ` AND status = $2`
		args = append(args, status)
	}
	query += ` ORDER BY ABS(COALESCE(delta, variance_amount)) DESC`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list variances: %w", err)
	}
	defer rows.Close()

	var out []Variance
	for rows.Next() {
		var v Variance
		if err := rows.Scan(&v.ID, &v.RunID, &v.VendorOrderID,
			&v.ExpectedAmount, &v.ActualAmount, &v.Delta,
			&v.VarianceType, &v.Suggestion, &v.Status); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if out == nil {
		out = []Variance{}
	}
	return out, nil
}

// SubmitVarianceForApproval transitions a variance from 'open' to
// 'pending_finance_approval'. Called by a non-finance user who has identified
// the variance and wants Finance to review it.
func (s *Store) SubmitVarianceForApproval(ctx context.Context, varianceID, submitterID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE reconciliation_variances
		SET status = 'pending_finance_approval'
		WHERE id = $1 AND status = 'open'`,
		varianceID)
	if err != nil {
		return fmt.Errorf("submit variance for approval: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("variance not found or not in open state")
	}
	return nil
}

// ApproveVariance transitions a variance from 'pending_finance_approval' to
// 'finance_approved'. The caller must hold the reconciliation:read (Finance)
// permission — permission enforcement is done at the handler/middleware level.
func (s *Store) ApproveVariance(ctx context.Context, varianceID, financeUserID string) error {
	now := time.Now()
	tag, err := s.pool.Exec(ctx, `
		UPDATE reconciliation_variances
		SET status = 'finance_approved',
		    finance_approved_by = $2,
		    finance_approved_at = $3
		WHERE id = $1 AND status = 'pending_finance_approval'`,
		varianceID, financeUserID, now)
	if err != nil {
		return fmt.Errorf("approve variance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("variance not found or not in pending_finance_approval state")
	}
	return nil
}

// ApplyApprovedVariance transitions a variance from 'finance_approved' to
// 'applied'. Returns an error if the variance has not yet been Finance-approved.
func (s *Store) ApplyApprovedVariance(ctx context.Context, varianceID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE reconciliation_variances
		SET status = 'applied'
		WHERE id = $1 AND status = 'finance_approved'`,
		varianceID)
	if err != nil {
		return fmt.Errorf("apply approved variance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("variance not found or not in finance_approved state — Finance approval is required before applying")
	}
	return nil
}

// ApplySuggestion is kept for backward compatibility; it now delegates to
// ApplyApprovedVariance to enforce the Finance gate.
func (s *Store) ApplySuggestion(ctx context.Context, varianceID string) error {
	return s.ApplyApprovedVariance(ctx, varianceID)
}

// ─────────────────────────────────────────────────────────────────────────────
// Settlement batches
// ─────────────────────────────────────────────────────────────────────────────

// CreateSettlementBatch creates a draft batch with lines. Validates allocations.
func (s *Store) CreateSettlementBatch(
	ctx context.Context,
	runID, createdBy string,
	lineItems []SettlementLineInput,
) (*SettlementBatch, error) {
	// Validate allocations sum to 100% per line.
	for i, li := range lineItems {
		if len(li.Allocations) == 0 {
			continue
		}
		var totalPct float64
		hasPct := false
		for _, a := range li.Allocations {
			if a.Percentage != nil {
				totalPct += *a.Percentage
				hasPct = true
			}
		}
		if hasPct && (totalPct < 99.99 || totalPct > 100.01) {
			return nil, fmt.Errorf("line %d allocations must sum to 100%% (got %.2f%%)", i, totalPct)
		}
	}

	batchID := uuid.New().String()
	now := time.Now()

	// Validate that runID references a real reconciliation_runs row.
	var exists bool
	_ = s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM reconciliation_runs WHERE id = $1)`, runID).
		Scan(&exists)
	if !exists {
		return nil, fmt.Errorf("reconciliation run %s not found", runID)
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO settlement_batches
			(id, run_id, status, created_by, created_at, updated_at)
		VALUES ($1, $2, 'draft', $3, $4, $4)`,
		batchID, runID, createdBy, now)
	if err != nil {
		return nil, fmt.Errorf("create settlement batch: %w", err)
	}

	for _, li := range lineItems {
		lineID := uuid.New().String()
		_, err = s.pool.Exec(ctx, `
			INSERT INTO settlement_lines
				(id, batch_id, order_id, amount, direction, cost_center_id)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			lineID, batchID, li.VendorOrderID,
			li.Amount, li.Direction, li.CostCenterID)
		if err != nil {
			return nil, fmt.Errorf("create settlement line: %w", err)
		}

		for _, a := range li.Allocations {
			_, err = s.pool.Exec(ctx, `
				INSERT INTO cost_allocations
					(settlement_line_id, department_code, cost_center, allocated_amount, allocated_pct)
				VALUES ($1, $2, $3, $4, $5)`,
				lineID, a.DepartmentCode, a.CostCenter,
				nullableInt64(a.Amount), a.Percentage)
			if err != nil {
				return nil, fmt.Errorf("create allocation: %w", err)
			}
		}
	}

	return s.GetSettlementBatch(ctx, batchID)
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullableInt64(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

// GetSettlementBatch fetches a batch with its lines and allocations.
func (s *Store) GetSettlementBatch(ctx context.Context, batchID string) (*SettlementBatch, error) {
	b := &SettlementBatch{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, run_id::TEXT, status,
		       created_by::TEXT, finance_approved_by::TEXT,
		       created_at, updated_at, approved_at, exported_at
		FROM settlement_batches WHERE id = $1`, batchID).
		Scan(&b.ID, &b.RunID, &b.Status,
			&b.CreatedBy, &b.FinanceApprovedBy,
			&b.CreatedAt, &b.UpdatedAt, &b.ApprovedAt, &b.ExportedAt)
	if err != nil {
		return nil, fmt.Errorf("get settlement batch: %w", err)
	}

	b.Lines = s.getBatchLines(ctx, batchID)
	return b, nil
}

func (s *Store) getBatchLines(ctx context.Context, batchID string) []SettlementLine {
	rows, err := s.pool.Query(ctx, `
		SELECT id, batch_id, order_id::TEXT, amount,
		       COALESCE(direction, 'AP'), COALESCE(cost_center_id, '')
		FROM settlement_lines
		WHERE batch_id = $1
		ORDER BY id`, batchID)
	if err != nil || rows == nil {
		return []SettlementLine{}
	}
	defer rows.Close()

	var lines []SettlementLine
	for rows.Next() {
		var l SettlementLine
		if err := rows.Scan(&l.ID, &l.BatchID, &l.VendorOrderID,
			&l.Amount, &l.Direction, &l.CostCenterID); err != nil {
			continue
		}
		l.Allocations = s.getLineAllocations(ctx, l.ID)
		lines = append(lines, l)
	}
	if lines == nil {
		return []SettlementLine{}
	}
	return lines
}

func (s *Store) getLineAllocations(ctx context.Context, lineID string) []CostAllocation {
	rows, err := s.pool.Query(ctx, `
		SELECT id, settlement_line_id::TEXT,
		       COALESCE(department_code, ''),
		       COALESCE(cost_center, ''),
		       COALESCE(allocated_amount, 0),
		       allocated_pct
		FROM cost_allocations
		WHERE settlement_line_id = $1
		ORDER BY id`, lineID)
	if err != nil || rows == nil {
		return nil
	}
	defer rows.Close()

	var out []CostAllocation
	for rows.Next() {
		var a CostAllocation
		if err := rows.Scan(&a.ID, &a.LineID, &a.DepartmentCode,
			&a.CostCenter, &a.Amount, &a.Percentage); err != nil {
			continue
		}
		out = append(out, a)
	}
	return out
}

// ListSettlementBatches returns batches filtered by run_id and/or status.
func (s *Store) ListSettlementBatches(ctx context.Context, runID, status string) ([]SettlementBatch, error) {
	query := `
		SELECT id, run_id::TEXT, status,
		       created_by::TEXT, finance_approved_by::TEXT,
		       created_at, updated_at, approved_at, exported_at
		FROM settlement_batches
		WHERE 1=1`
	args := []any{}
	argN := 1

	if runID != "" {
		query += fmt.Sprintf(` AND run_id = $%d`, argN)
		args = append(args, runID)
		argN++
	}
	if status != "" {
		query += fmt.Sprintf(` AND status = $%d`, argN)
		args = append(args, status)
		argN++
	}
	query += ` ORDER BY created_at DESC`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list batches: %w", err)
	}
	defer rows.Close()

	var out []SettlementBatch
	for rows.Next() {
		var b SettlementBatch
		if err := rows.Scan(&b.ID, &b.RunID, &b.Status,
			&b.CreatedBy, &b.FinanceApprovedBy,
			&b.CreatedAt, &b.UpdatedAt, &b.ApprovedAt, &b.ExportedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if out == nil {
		out = []SettlementBatch{}
	}
	return out, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Settlement state machine transitions
// ─────────────────────────────────────────────────────────────────────────────

// SubmitForApproval transitions a batch from draft → under_review.
func (s *Store) SubmitForApproval(ctx context.Context, batchID string) error {
	return s.transition(ctx, batchID, "draft", "under_review")
}

// ApproveBatch transitions a batch from under_review → approved and atomically
// generates AR and AP entries from the batch's settlement lines.
func (s *Store) ApproveBatch(ctx context.Context, batchID, approvedByID string) error {
	now := time.Now()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Transition the batch status.
	tag, err := tx.Exec(ctx, `
		UPDATE settlement_batches
		SET status = 'approved',
		    finance_approved_by = $2,
		    approved_at = $3,
		    updated_at = $3
		WHERE id = $1 AND status = 'under_review'`,
		batchID, approvedByID, now)
	if err != nil {
		return fmt.Errorf("approve batch: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("batch not found or not in under_review state")
	}

	// Generate AR entries from AR-direction settlement lines.
	_, err = tx.Exec(ctx, `
		INSERT INTO ar_entries
			(id, settlement_line_id, settlement_batch_id, amount, direction, generated_at, batch_id)
		SELECT gen_random_uuid(), sl.id, sl.batch_id, sl.amount, 'AR', NOW(), sl.batch_id
		FROM settlement_lines sl
		WHERE sl.batch_id = $1
		  AND UPPER(COALESCE(sl.direction, 'AP')) = 'AR'`, batchID)
	if err != nil {
		return fmt.Errorf("generate AR entries: %w", err)
	}

	// Generate AP entries from AP-direction settlement lines.
	_, err = tx.Exec(ctx, `
		INSERT INTO ap_entries
			(id, settlement_line_id, settlement_batch_id, amount, direction, generated_at, batch_id)
		SELECT gen_random_uuid(), sl.id, sl.batch_id, sl.amount, 'AP', NOW(), sl.batch_id
		FROM settlement_lines sl
		WHERE sl.batch_id = $1
		  AND UPPER(COALESCE(sl.direction, 'AP')) = 'AP'`, batchID)
	if err != nil {
		return fmt.Errorf("generate AP entries: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit approve batch: %w", err)
	}

	s.emitWebhook(ctx, "settlement.approved", map[string]any{
		"batch_id":           batchID,
		"finance_approved_by": approvedByID,
		"approved_at":        now.UTC().Format(time.RFC3339),
	})
	return nil
}

// ExportBatch transitions approved → exported and returns CSV bytes.
func (s *Store) ExportBatch(ctx context.Context, batchID string) ([]byte, error) {
	var status string
	err := s.pool.QueryRow(ctx,
		`SELECT status FROM settlement_batches WHERE id = $1`, batchID).Scan(&status)
	if err != nil {
		return nil, fmt.Errorf("batch not found: %w", err)
	}
	if status != "approved" {
		return nil, fmt.Errorf("batch must be in approved state to export (current: %s)", status)
	}

	batch, err := s.GetSettlementBatch(ctx, batchID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	_, err = s.pool.Exec(ctx, `
		UPDATE settlement_batches
		SET status = 'exported', exported_at = $1, updated_at = $1
		WHERE id = $2`, now, batchID)
	if err != nil {
		return nil, fmt.Errorf("mark exported: %w", err)
	}

	// Build CSV — includes allocation breakdowns (one sub-row per allocation)
	// so downstream finance systems receive department and cost-center detail.
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{
		"batch_id", "line_id", "vendor_order_id", "amount",
		"direction", "cost_center_id",
		"alloc_department_code", "alloc_cost_center", "alloc_amount", "alloc_pct",
		"exported_at",
	})
	exportedAtStr := now.UTC().Format(time.RFC3339)
	for _, l := range batch.Lines {
		orderID := ""
		if l.VendorOrderID != nil {
			orderID = *l.VendorOrderID
		}
		if len(l.Allocations) == 0 {
			// No sub-allocations — emit one row with empty allocation fields.
			_ = w.Write([]string{
				batchID, l.ID, orderID,
				fmt.Sprintf("%d", l.Amount), l.Direction, l.CostCenterID,
				"", "", "", "",
				exportedAtStr,
			})
		} else {
			for _, a := range l.Allocations {
				pct := ""
				if a.Percentage != nil {
					pct = fmt.Sprintf("%.4f", *a.Percentage)
				}
				_ = w.Write([]string{
					batchID, l.ID, orderID,
					fmt.Sprintf("%d", l.Amount), l.Direction, l.CostCenterID,
					a.DepartmentCode, a.CostCenter,
					fmt.Sprintf("%d", a.Amount), pct,
					exportedAtStr,
				})
			}
		}
	}
	w.Flush()

	s.emitWebhook(ctx, "settlement.exported", map[string]any{
		"batch_id":    batchID,
		"line_count":  len(batch.Lines),
		"exported_at": now.UTC().Format(time.RFC3339),
	})
	return buf.Bytes(), nil
}

// SettleBatch transitions exported → settled.
func (s *Store) SettleBatch(ctx context.Context, batchID string) error {
	if err := s.transition(ctx, batchID, "exported", "settled"); err != nil {
		return err
	}
	s.emitWebhook(ctx, "settlement.settled", map[string]any{
		"batch_id":   batchID,
		"settled_at": time.Now().UTC().Format(time.RFC3339),
	})
	return nil
}

// VoidBatch voids a batch from draft, under_review, or exception status.
func (s *Store) VoidBatch(ctx context.Context, batchID, reason string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE settlement_batches
		SET status = 'voided', void_reason = $2, updated_at = NOW()
		WHERE id = $1 AND status IN ('draft', 'under_review', 'exception')`,
		batchID, reason)
	if err != nil {
		return fmt.Errorf("void batch: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("batch not found or cannot be voided from current state")
	}
	return nil
}

// transition performs a generic single-step status transition.
func (s *Store) transition(ctx context.Context, batchID, from, to string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE settlement_batches
		SET status = $3, updated_at = NOW()
		WHERE id = $1 AND status = $2`,
		batchID, from, to)
	if err != nil {
		return fmt.Errorf("transition %s→%s: %w", from, to, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("batch not found or not in %s state", from)
	}
	return nil
}
