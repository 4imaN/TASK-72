-- Fix: make statement_row_id nullable for missing-statement variances
ALTER TABLE reconciliation_variances
  ALTER COLUMN statement_row_id DROP NOT NULL;

-- Fix: add description and rejection_reason to vendor_orders
ALTER TABLE vendor_orders
  ADD COLUMN IF NOT EXISTS description      TEXT,
  ADD COLUMN IF NOT EXISTS rejection_reason TEXT;

-- Fix: remove transitional recon_run_id alias column from settlement_batches.
-- settlement_batches.run_id already references reconciliation_runs(id) directly;
-- recon_run_id was added by migration 005 as a redundant alias to the now-dropped
-- recon_runs table (which is a VIEW). Dropping it normalises the model to one FK.
ALTER TABLE settlement_batches
  DROP COLUMN IF EXISTS recon_run_id;
