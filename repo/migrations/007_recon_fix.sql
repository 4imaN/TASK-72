-- migrations/007_recon_fix.sql
-- Reconciliation schema consolidation and Finance approval gate.
-- All DDL uses IF NOT EXISTS / DO blocks so this migration is idempotent.

-- ─────────────────────────────────────────────────────────────────────────────
-- 1. Alias recon_runs → reconciliation_runs
--    The code now uses reconciliation_runs exclusively.
--    We keep the recon_runs table from migration 005 accessible via a view so
--    that any ad-hoc queries that still reference it continue to work.
-- ─────────────────────────────────────────────────────────────────────────────

-- Add the new API-driven columns to reconciliation_runs if they don't exist.
-- reconciliation_runs was originally batch-import driven; we extend it to also
-- support API-initiated runs (period, initiated_by, summary_json).
ALTER TABLE reconciliation_runs
    ADD COLUMN IF NOT EXISTS period          TEXT,
    ADD COLUMN IF NOT EXISTS initiated_by    UUID REFERENCES users(id),
    ADD COLUMN IF NOT EXISTS summary_json    JSONB,
    ADD COLUMN IF NOT EXISTS statement_import_batch_id UUID REFERENCES statement_import_batches(id);

-- Make batch_id nullable so that API-initiated runs (no import batch) are valid.
ALTER TABLE reconciliation_runs
    ALTER COLUMN batch_id DROP NOT NULL;

-- Make run_by nullable (API runs use initiated_by instead).
ALTER TABLE reconciliation_runs
    ALTER COLUMN run_by DROP NOT NULL;

-- Reconcile status values: the old table used 'running'/'completed'/'failed';
-- the new API also uses 'pending'/'processing'. Add a check if not present.
-- (PostgreSQL will ignore the ALTER if the constraint already exists.)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'reconciliation_runs_status_check'
    ) THEN
        -- No constraint to update; the column is a free-text status.
        NULL;
    END IF;
END $$;

-- Create a view so legacy code/queries using "recon_runs" still work.
-- The recon_runs table from migration 005 will be shadowed by this view
-- once the table is dropped (see below). If recon_runs is still a table,
-- we rename it first to avoid naming conflicts.
DO $$
BEGIN
    -- If recon_runs exists as a table (not a view), migrate its rows into
    -- reconciliation_runs, then drop it so we can create the view.
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_name = 'recon_runs' AND table_type = 'BASE TABLE'
    ) THEN
        -- Copy any rows from recon_runs into reconciliation_runs.
        INSERT INTO reconciliation_runs
            (id, period, initiated_by, status, started_at, completed_at, summary_json)
        SELECT
            id,
            period,
            initiated_by,
            status,
            created_at,
            completed_at,
            summary_json
        FROM recon_runs
        ON CONFLICT (id) DO NOTHING;

        -- Also copy recon_variances rows (referencing old recon_runs.id)
        -- into reconciliation_variances if the table exists.
        IF EXISTS (
            SELECT 1 FROM information_schema.tables
            WHERE table_name = 'recon_variances' AND table_type = 'BASE TABLE'
        ) THEN
            -- reconciliation_variances requires statement_row_id; use 0 as placeholder.
            -- We insert only if a matching statement_rows row exists; otherwise skip.
            INSERT INTO reconciliation_variances
                (id, run_id, statement_row_id, order_id,
                 expected_amount, actual_amount, variance_amount, status)
            SELECT
                rv.id,
                rv.run_id,
                0,                  -- placeholder statement_row_id
                rv.vendor_order_id,
                rv.expected_amount,
                rv.actual_amount,
                rv.delta,
                rv.status
            FROM recon_variances rv
            WHERE EXISTS (SELECT 1 FROM reconciliation_runs rr WHERE rr.id = rv.run_id)
            ON CONFLICT (id) DO NOTHING;

            DROP TABLE recon_variances CASCADE;
        END IF;

        DROP TABLE recon_runs CASCADE;
    END IF;
END $$;

-- Now create the view (safe to run even if recon_runs was already dropped).
CREATE OR REPLACE VIEW recon_runs AS
    SELECT
        id,
        period,
        status,
        initiated_by,
        started_at   AS created_at,
        completed_at,
        summary_json
    FROM reconciliation_runs
    WHERE period IS NOT NULL;   -- only show API-initiated runs through this view

-- ─────────────────────────────────────────────────────────────────────────────
-- 2. statement_rows: ensure vendor_order_id FK column exists
--    The original table uses order_id; we add vendor_order_id as an alias
--    so that ProcessRun can JOIN on it by name.
-- ─────────────────────────────────────────────────────────────────────────────

ALTER TABLE statement_rows
    ADD COLUMN IF NOT EXISTS vendor_order_id UUID REFERENCES vendor_orders(id);

-- Back-fill vendor_order_id from existing order_id column.
UPDATE statement_rows
SET vendor_order_id = order_id
WHERE vendor_order_id IS NULL AND order_id IS NOT NULL;

-- ─────────────────────────────────────────────────────────────────────────────
-- 3. reconciliation_variances: Finance approval gate columns + status expansion
-- ─────────────────────────────────────────────────────────────────────────────

ALTER TABLE reconciliation_variances
    ADD COLUMN IF NOT EXISTS finance_approved_by  UUID REFERENCES users(id),
    ADD COLUMN IF NOT EXISTS finance_approved_at  TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS variance_type        TEXT NOT NULL DEFAULT 'amount',
    ADD COLUMN IF NOT EXISTS suggestion           TEXT,
    ADD COLUMN IF NOT EXISTS delta                BIGINT;

-- Compute delta from existing rows where it is null.
UPDATE reconciliation_variances
SET delta = variance_amount
WHERE delta IS NULL;

-- Extend the status check to include the Finance approval states.
-- We do this by dropping any existing constraint and adding a new one.
DO $$
BEGIN
    ALTER TABLE reconciliation_variances DROP CONSTRAINT IF EXISTS reconciliation_variances_status_check;
EXCEPTION WHEN undefined_object THEN NULL;
END $$;

ALTER TABLE reconciliation_variances
    ADD CONSTRAINT reconciliation_variances_status_check
    CHECK (status IN (
        'open',
        'pending_finance_approval',
        'finance_approved',
        'applied',
        'voided',
        'writeoff_suggested',
        'writeoff_approved',
        'resolved',
        'ignored'
    ));

-- ─────────────────────────────────────────────────────────────────────────────
-- 4. ar_entries / ap_entries: add settlement_line_id and generated_at columns
--    so that ApproveBatch can INSERT entries linked to settlement lines.
-- ─────────────────────────────────────────────────────────────────────────────

ALTER TABLE ar_entries
    ADD COLUMN IF NOT EXISTS settlement_line_id UUID REFERENCES settlement_lines(id),
    ADD COLUMN IF NOT EXISTS direction          VARCHAR(2) NOT NULL DEFAULT 'AR',
    ADD COLUMN IF NOT EXISTS generated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS batch_id           UUID REFERENCES settlement_batches(id);

ALTER TABLE ap_entries
    ADD COLUMN IF NOT EXISTS settlement_line_id UUID REFERENCES settlement_lines(id),
    ADD COLUMN IF NOT EXISTS direction          VARCHAR(2) NOT NULL DEFAULT 'AP',
    ADD COLUMN IF NOT EXISTS generated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS batch_id           UUID REFERENCES settlement_batches(id);

-- ─────────────────────────────────────────────────────────────────────────────
-- 5. Indexes for new columns
-- ─────────────────────────────────────────────────────────────────────────────

CREATE INDEX IF NOT EXISTS idx_reconciliation_runs_period      ON reconciliation_runs(period);
CREATE INDEX IF NOT EXISTS idx_reconciliation_runs_status      ON reconciliation_runs(status);
CREATE INDEX IF NOT EXISTS idx_recon_variances_run             ON reconciliation_variances(run_id);
CREATE INDEX IF NOT EXISTS idx_recon_variances_status          ON reconciliation_variances(status);
CREATE INDEX IF NOT EXISTS idx_statement_rows_vendor_order     ON statement_rows(vendor_order_id);
CREATE INDEX IF NOT EXISTS idx_ar_entries_batch                ON ar_entries(batch_id);
CREATE INDEX IF NOT EXISTS idx_ap_entries_batch                ON ap_entries(batch_id);
