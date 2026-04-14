-- migrations/006_exports_config.sql
-- Adds columns needed by Slice 9: exports, config center (rollout_pct, target_roles),
-- version rules (max_version, action, message), and webhook delivery tables.

-- ─────────────────────────────────────────────────────────────────────────────
-- export_jobs: add type, created_by, params_json, file_path, error_msg columns
-- (The existing table uses different column names; we add the Slice 9 columns)
-- ─────────────────────────────────────────────────────────────────────────────

ALTER TABLE export_jobs
    ADD COLUMN IF NOT EXISTS job_type       TEXT,
    ADD COLUMN IF NOT EXISTS created_by_s9  TEXT,
    ADD COLUMN IF NOT EXISTS params_json    JSONB,
    ADD COLUMN IF NOT EXISTS file_path      TEXT,
    ADD COLUMN IF NOT EXISTS error_msg      TEXT;

-- Ensure status column has 'queued' as a valid value (it was 'pending' before)
-- We store new jobs with status='queued' consistent with Slice 9 spec.
-- The column already allows free-form TEXT so no constraint change needed.

-- ─────────────────────────────────────────────────────────────────────────────
-- config_flags: add rollout_percentage and target_roles columns
-- ─────────────────────────────────────────────────────────────────────────────

ALTER TABLE config_flags
    ADD COLUMN IF NOT EXISTS rollout_percentage INT     NOT NULL DEFAULT 100,
    ADD COLUMN IF NOT EXISTS target_roles       TEXT[]  NOT NULL DEFAULT '{}';

-- ─────────────────────────────────────────────────────────────────────────────
-- client_version_rules: add max_version, action, message columns
-- (Slice 1 only had min_version / is_blocked / grace_until)
-- ─────────────────────────────────────────────────────────────────────────────

ALTER TABLE client_version_rules
    ADD COLUMN IF NOT EXISTS max_version TEXT,
    ADD COLUMN IF NOT EXISTS action      TEXT NOT NULL DEFAULT 'block',
    ADD COLUMN IF NOT EXISTS message     TEXT;

-- ─────────────────────────────────────────────────────────────────────────────
-- webhook_endpoints: new table for Slice 9 webhook management
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS webhook_endpoints (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    url             TEXT        NOT NULL,
    events          TEXT[]      NOT NULL DEFAULT '{}',
    is_active       BOOLEAN     NOT NULL DEFAULT TRUE,
    created_by      TEXT        NOT NULL DEFAULT '',
    secret_enc      TEXT        NOT NULL DEFAULT '',   -- AES-encrypted secret
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────────────────────────────────────
-- webhook_deliveries: delivery tracking for webhook events
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    endpoint_id     UUID        NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    event_type      TEXT        NOT NULL,
    payload_json    JSONB       NOT NULL DEFAULT '{}',
    status          TEXT        NOT NULL DEFAULT 'pending',  -- pending, delivered, failed
    attempts        INT         NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMPTZ,
    response_status INT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_endpoint ON webhook_deliveries(endpoint_id);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_status   ON webhook_deliveries(status);
