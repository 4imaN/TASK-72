-- migrations/005_reconciliation.sql
-- Adds reconciliation runs v2, billing rules seed data, and settlement columns.

-- ─────────────────────────────────────────────────────────────────────────────
-- Reconciliation runs v2 (API-driven, not import-batch-driven)
-- We add a separate table so the existing reconciliation_runs is untouched.
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS recon_runs (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    period          TEXT        NOT NULL,            -- YYYY-MM
    status          TEXT        NOT NULL DEFAULT 'pending',  -- pending, processing, completed, failed
    initiated_by    UUID        NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    summary_json    JSONB
);

CREATE INDEX IF NOT EXISTS idx_recon_runs_period ON recon_runs(period);
CREATE INDEX IF NOT EXISTS idx_recon_runs_status ON recon_runs(status);

-- ─────────────────────────────────────────────────────────────────────────────
-- Reconciliation variances v2
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS recon_variances (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID        NOT NULL REFERENCES recon_runs(id) ON DELETE CASCADE,
    vendor_order_id UUID        NOT NULL REFERENCES vendor_orders(id),
    expected_amount BIGINT      NOT NULL,    -- minor units
    actual_amount   BIGINT      NOT NULL,
    delta           BIGINT      NOT NULL,    -- actual - expected (signed)
    variance_type   TEXT        NOT NULL DEFAULT 'amount', -- 'amount', 'missing', 'extra'
    suggestion      TEXT,                   -- human-readable suggestion text
    status          TEXT        NOT NULL DEFAULT 'open'  -- open, applied, ignored
);

CREATE INDEX IF NOT EXISTS idx_recon_variances_run ON recon_variances(run_id);
CREATE INDEX IF NOT EXISTS idx_recon_variances_status ON recon_variances(status);

-- ─────────────────────────────────────────────────────────────────────────────
-- Settlement batches v2 (adding finance_approved_by, approved_at, exported_at)
-- ─────────────────────────────────────────────────────────────────────────────

ALTER TABLE settlement_batches
    ADD COLUMN IF NOT EXISTS finance_approved_by  UUID REFERENCES users(id),
    ADD COLUMN IF NOT EXISTS approved_at          TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS exported_at          TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS void_reason          TEXT,
    ADD COLUMN IF NOT EXISTS recon_run_id         UUID REFERENCES recon_runs(id);

-- ─────────────────────────────────────────────────────────────────────────────
-- Settlement lines v2 (adding direction and cost_center_id columns)
-- ─────────────────────────────────────────────────────────────────────────────

ALTER TABLE settlement_lines
    ADD COLUMN IF NOT EXISTS direction      TEXT NOT NULL DEFAULT 'AP', -- 'AR' or 'AP'
    ADD COLUMN IF NOT EXISTS cost_center_id TEXT;

-- ─────────────────────────────────────────────────────────────────────────────
-- Billing rule seed data
-- ─────────────────────────────────────────────────────────────────────────────

INSERT INTO billing_rule_sets (name, description, is_active) VALUES
    ('standard_vendor',       'Standard vendor billing rules',           TRUE),
    ('premium_vendor',        'Premium vendor with volume discounts',    TRUE),
    ('training_provider',     'Training and certification providers',    TRUE)
ON CONFLICT (name) DO NOTHING;

DO $$
DECLARE
    v_standard_id BIGINT;
    v_premium_id  BIGINT;
    v_training_id BIGINT;
BEGIN
    SELECT id INTO v_standard_id  FROM billing_rule_sets WHERE name = 'standard_vendor';
    SELECT id INTO v_premium_id   FROM billing_rule_sets WHERE name = 'premium_vendor';
    SELECT id INTO v_training_id  FROM billing_rule_sets WHERE name = 'training_provider';

    -- Standard vendor v1
    INSERT INTO billing_rule_versions
        (rule_set_id, version_number, effective_from, rule_definition)
    VALUES
        (v_standard_id, 1, '2024-01-01', '{"payment_terms_days":30,"late_fee_pct":1.5,"variance_threshold_pct":2.0,"auto_writeoff_limit":500}')
    ON CONFLICT (rule_set_id, version_number) DO NOTHING;

    -- Standard vendor v2 (updated thresholds)
    INSERT INTO billing_rule_versions
        (rule_set_id, version_number, effective_from, rule_definition)
    VALUES
        (v_standard_id, 2, '2025-01-01', '{"payment_terms_days":30,"late_fee_pct":1.0,"variance_threshold_pct":2.0,"auto_writeoff_limit":1000}')
    ON CONFLICT (rule_set_id, version_number) DO NOTHING;

    -- Premium vendor v1
    INSERT INTO billing_rule_versions
        (rule_set_id, version_number, effective_from, rule_definition)
    VALUES
        (v_premium_id, 1, '2024-01-01', '{"payment_terms_days":45,"late_fee_pct":0.5,"variance_threshold_pct":1.5,"volume_discount_tiers":[{"min_amount":100000,"discount_pct":5},{"min_amount":500000,"discount_pct":10}]}')
    ON CONFLICT (rule_set_id, version_number) DO NOTHING;

    -- Training provider v1
    INSERT INTO billing_rule_versions
        (rule_set_id, version_number, effective_from, rule_definition)
    VALUES
        (v_training_id, 1, '2024-01-01', '{"payment_terms_days":60,"late_fee_pct":0.0,"variance_threshold_pct":3.0,"includes_platform_fee":true,"platform_fee_pct":2.5}')
    ON CONFLICT (rule_set_id, version_number) DO NOTHING;
END $$;
