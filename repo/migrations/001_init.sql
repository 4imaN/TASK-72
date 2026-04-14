-- migrations/001_init.sql
-- Foundation schema: migration tracking, identity, and core tables.
-- Extensions are enabled by init_db.sh before this runs.

-- ─────────────────────────────────────────────────────────────────────────────
-- Migration and seed tracking tables (must exist first)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS schema_migrations (
    id          BIGSERIAL PRIMARY KEY,
    filename    TEXT        NOT NULL UNIQUE,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS seed_runs (
    id          BIGSERIAL PRIMARY KEY,
    filename    TEXT        NOT NULL UNIQUE,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Identity and access
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS roles (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT        NOT NULL UNIQUE,   -- learner, procurement, approver, finance, moderator, admin
    display_name TEXT       NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS permissions (
    id          BIGSERIAL PRIMARY KEY,
    code        TEXT        NOT NULL UNIQUE,   -- e.g. catalog:read, orders:write
    description TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id       BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id BIGINT NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE IF NOT EXISTS users (
    id                      UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    username                TEXT        NOT NULL UNIQUE,
    email                   TEXT        NOT NULL UNIQUE,
    display_name            TEXT        NOT NULL,
    password_hash           TEXT        NOT NULL,
    force_password_reset    BOOLEAN     NOT NULL DEFAULT FALSE,
    is_active               BOOLEAN     NOT NULL DEFAULT TRUE,
    job_family_id           BIGINT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at           TIMESTAMPTZ,
    -- Personal data masking: these columns are encrypted at rest in sensitive contexts
    -- For Slice 1 they store plaintext; encryption layer added in Slice 3
    recovery_email          TEXT,       -- masked in non-admin views
    phone                   TEXT        -- masked in non-admin views
);

CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
CREATE INDEX IF NOT EXISTS idx_users_email    ON users(email);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id   UUID   NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id   BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    granted_by UUID REFERENCES users(id),
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS sessions (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash      TEXT        NOT NULL UNIQUE,  -- SHA-256 of opaque token
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_active_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL,         -- absolute max (8 hours from created_at)
    idle_expires_at TIMESTAMPTZ NOT NULL,         -- 15 min from last_active_at
    is_invalidated  BOOLEAN     NOT NULL DEFAULT FALSE,
    client_version  TEXT,                         -- for compatibility checks
    ip_address      TEXT,
    user_agent      TEXT
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id    ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_token_hash ON sessions(token_hash);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at) WHERE NOT is_invalidated;

CREATE TABLE IF NOT EXISTS mfa_totp_enrollments (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE UNIQUE,
    -- secret stored encrypted at rest (Slice 3 adds encryption)
    encrypted_secret TEXT       NOT NULL,
    confirmed        BOOLEAN    NOT NULL DEFAULT FALSE,
    enrolled_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at     TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS mfa_recovery_codes (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- code_hash is bcrypt of the recovery code; codes encrypted at rest
    code_hash   TEXT        NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mfa_recovery_codes_user ON mfa_recovery_codes(user_id);

CREATE TABLE IF NOT EXISTS password_reset_events (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    actor_id    UUID        REFERENCES users(id),
    reason      TEXT        NOT NULL,  -- 'bootstrap_rotation', 'admin_reset', 'user_request'
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS access_reveal_logs (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id    UUID        NOT NULL REFERENCES users(id),
    target_user_id UUID     REFERENCES users(id),
    field_name  TEXT        NOT NULL,
    reason      TEXT        NOT NULL,
    ip_address  TEXT,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Job families and taxonomy root
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS job_families (
    id          BIGSERIAL PRIMARY KEY,
    code        TEXT        NOT NULL UNIQUE,
    name        TEXT        NOT NULL,
    description TEXT,
    is_active   BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS skill_tags (
    id              BIGSERIAL PRIMARY KEY,
    code            TEXT        NOT NULL UNIQUE,
    canonical_name  TEXT        NOT NULL,
    parent_id       BIGINT REFERENCES skill_tags(id),
    is_active       BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_skill_tags_parent ON skill_tags(parent_id);

CREATE TABLE IF NOT EXISTS tag_synonyms (
    id              BIGSERIAL PRIMARY KEY,
    tag_id          BIGINT      NOT NULL REFERENCES skill_tags(id) ON DELETE CASCADE,
    synonym_text    TEXT        NOT NULL,
    synonym_type    TEXT        NOT NULL DEFAULT 'alias', -- 'alias', 'pinyin', 'abbreviation'
    is_active       BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (synonym_text, synonym_type)
);

CREATE INDEX IF NOT EXISTS idx_tag_synonyms_tag  ON tag_synonyms(tag_id);
CREATE INDEX IF NOT EXISTS idx_tag_synonyms_text ON tag_synonyms(synonym_text);

CREATE TABLE IF NOT EXISTS tag_conflicts (
    id              BIGSERIAL PRIMARY KEY,
    synonym_text    TEXT        NOT NULL,
    tag_id_a        BIGINT      NOT NULL REFERENCES skill_tags(id),
    tag_id_b        BIGINT      NOT NULL REFERENCES skill_tags(id),
    detected_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at     TIMESTAMPTZ,
    resolved_by     UUID REFERENCES users(id),
    resolution      TEXT        -- 'deactivated_a', 'deactivated_b', 'merged'
);

CREATE TABLE IF NOT EXISTS taxonomy_review_queue (
    id              BIGSERIAL PRIMARY KEY,
    conflict_id     BIGINT      REFERENCES tag_conflicts(id),
    status          TEXT        NOT NULL DEFAULT 'pending', -- 'pending', 'reviewed', 'escalated'
    assigned_to     UUID        REFERENCES users(id),
    notes           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Catalog: resources
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS resources (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    title           TEXT        NOT NULL,
    description     TEXT,
    content_type    TEXT        NOT NULL DEFAULT 'article', -- 'article','video','course','document'
    source_url      TEXT,
    source_id       TEXT,       -- external source identifier for dedup
    content_checksum TEXT,      -- for duplicate detection
    job_family_id   BIGINT      REFERENCES job_families(id),
    category        TEXT        NOT NULL DEFAULT 'general',
    publish_date    DATE,
    is_published    BOOLEAN     NOT NULL DEFAULT FALSE,
    is_archived     BOOLEAN     NOT NULL DEFAULT FALSE,
    view_count      BIGINT      NOT NULL DEFAULT 0,
    created_by      UUID        REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_resources_category     ON resources(category);
CREATE INDEX IF NOT EXISTS idx_resources_publish_date ON resources(publish_date);
CREATE INDEX IF NOT EXISTS idx_resources_job_family   ON resources(job_family_id);
CREATE INDEX IF NOT EXISTS idx_resources_published    ON resources(is_published, is_archived);
-- Full-text search index
CREATE INDEX IF NOT EXISTS idx_resources_fts ON resources USING gin(to_tsvector('english', coalesce(title,'') || ' ' || coalesce(description,'')));
-- Trigram indexes for fuzzy title search
CREATE INDEX IF NOT EXISTS idx_resources_title_trgm ON resources USING gin(title gin_trgm_ops);

CREATE TABLE IF NOT EXISTS resource_tags (
    resource_id UUID   NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
    tag_id      BIGINT NOT NULL REFERENCES skill_tags(id) ON DELETE CASCADE,
    PRIMARY KEY (resource_id, tag_id)
);

CREATE TABLE IF NOT EXISTS resource_duplicate_groups (
    id              BIGSERIAL PRIMARY KEY,
    canonical_id    UUID        NOT NULL REFERENCES resources(id),
    duplicate_id    UUID        NOT NULL REFERENCES resources(id),
    detected_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reviewed_at     TIMESTAMPTZ,
    reviewed_by     UUID        REFERENCES users(id),
    status          TEXT        NOT NULL DEFAULT 'pending', -- 'pending', 'confirmed', 'dismissed'
    UNIQUE (canonical_id, duplicate_id)
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Search index tables
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS search_documents (
    id              BIGSERIAL PRIMARY KEY,
    resource_id     UUID        NOT NULL REFERENCES resources(id) ON DELETE CASCADE UNIQUE,
    title_tokens    TSVECTOR,
    body_tokens     TSVECTOR,
    combined_tokens TSVECTOR,   -- weighted: title A, body B
    popularity_score FLOAT      NOT NULL DEFAULT 0,
    last_rebuilt_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_search_docs_combined ON search_documents USING gin(combined_tokens);
CREATE INDEX IF NOT EXISTS idx_search_docs_resource  ON search_documents(resource_id);

CREATE TABLE IF NOT EXISTS search_rebuild_runs (
    id              BIGSERIAL PRIMARY KEY,
    triggered_by    TEXT        NOT NULL DEFAULT 'scheduler', -- 'scheduler', 'manual', 'incremental'
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    doc_count       BIGINT,
    error_summary   TEXT,
    status          TEXT        NOT NULL DEFAULT 'running' -- 'running', 'completed', 'failed'
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Archive browsing
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS archive_buckets (
    id              BIGSERIAL PRIMARY KEY,
    bucket_type     TEXT        NOT NULL, -- 'month', 'tag'
    bucket_key      TEXT        NOT NULL, -- '2024-01' or tag code
    display_label   TEXT        NOT NULL,
    resource_count  INT         NOT NULL DEFAULT 0,
    last_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (bucket_type, bucket_key)
);

CREATE TABLE IF NOT EXISTS archive_membership (
    resource_id UUID   NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
    bucket_id   BIGINT NOT NULL REFERENCES archive_buckets(id) ON DELETE CASCADE,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (resource_id, bucket_id)
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Learning paths and progress
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS learning_paths (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    title           TEXT        NOT NULL,
    description     TEXT,
    job_family_id   BIGINT      REFERENCES job_families(id),
    is_published    BOOLEAN     NOT NULL DEFAULT FALSE,
    created_by      UUID        REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS learning_path_items (
    id              BIGSERIAL PRIMARY KEY,
    path_id         UUID        NOT NULL REFERENCES learning_paths(id) ON DELETE CASCADE,
    resource_id     UUID        NOT NULL REFERENCES resources(id),
    item_type       TEXT        NOT NULL DEFAULT 'required', -- 'required', 'elective'
    sort_order      INT         NOT NULL DEFAULT 0,
    UNIQUE (path_id, resource_id)
);

CREATE TABLE IF NOT EXISTS learning_path_rules (
    id                      BIGSERIAL PRIMARY KEY,
    path_id                 UUID NOT NULL REFERENCES learning_paths(id) ON DELETE CASCADE UNIQUE,
    required_count          INT  NOT NULL DEFAULT 0,
    elective_minimum        INT  NOT NULL DEFAULT 0,
    -- completion: all required + at least elective_minimum electives
    completion_description  TEXT
);

CREATE TABLE IF NOT EXISTS learning_enrollments (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    path_id         UUID        NOT NULL REFERENCES learning_paths(id),
    enrolled_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    status          TEXT        NOT NULL DEFAULT 'active', -- 'active', 'completed', 'withdrawn'
    UNIQUE (user_id, path_id)
);

CREATE TABLE IF NOT EXISTS learning_progress_events (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    resource_id     UUID        NOT NULL REFERENCES resources(id),
    event_type      TEXT        NOT NULL, -- 'started', 'progress', 'completed', 'resumed'
    position_seconds INT,
    progress_pct    FLOAT,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    device_hint     TEXT
);

CREATE INDEX IF NOT EXISTS idx_progress_events_user     ON learning_progress_events(user_id);
CREATE INDEX IF NOT EXISTS idx_progress_events_resource ON learning_progress_events(resource_id);

CREATE TABLE IF NOT EXISTS learning_progress_snapshots (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    resource_id     UUID        NOT NULL REFERENCES resources(id),
    status          TEXT        NOT NULL DEFAULT 'not_started', -- 'not_started', 'in_progress', 'completed'
    progress_pct    FLOAT       NOT NULL DEFAULT 0,
    last_position_seconds INT   NOT NULL DEFAULT 0,
    last_active_at  TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    UNIQUE (user_id, resource_id)
);

CREATE TABLE IF NOT EXISTS learning_exports (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    requested_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    status          TEXT        NOT NULL DEFAULT 'pending',
    file_path       TEXT,
    checksum        TEXT,
    error_message   TEXT
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Recommendations
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS behavior_events (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    resource_id     UUID        NOT NULL REFERENCES resources(id),
    event_type      TEXT        NOT NULL, -- 'view', 'complete', 'click', 'search_click'
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_behavior_user     ON behavior_events(user_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_behavior_resource ON behavior_events(resource_id);

CREATE TABLE IF NOT EXISTS recommendation_impressions (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    resource_id     UUID        NOT NULL REFERENCES resources(id),
    carousel_slot   INT,
    generated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    clicked_at      TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS recommendation_trace_factors (
    id                  BIGSERIAL PRIMARY KEY,
    impression_id       UUID NOT NULL REFERENCES recommendation_impressions(id) ON DELETE CASCADE,
    factor_type         TEXT NOT NULL, -- 'job_family', 'tag_overlap', 'prior_completion', 'popularity', 'cold_start'
    factor_detail       TEXT,          -- human-readable explanation
    weight              FLOAT NOT NULL DEFAULT 1.0
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Procurement: orders, reviews, disputes
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS vendor_orders (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    vendor_name     TEXT        NOT NULL,
    order_number    TEXT        NOT NULL UNIQUE,
    order_date      DATE        NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'pending', -- 'pending','received','disputed','closed'
    total_amount    BIGINT      NOT NULL DEFAULT 0,  -- integer minor units
    currency        TEXT        NOT NULL DEFAULT 'USD',
    created_by      UUID        REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_orders_vendor  ON vendor_orders(vendor_name);
CREATE INDEX IF NOT EXISTS idx_orders_status  ON vendor_orders(status);

CREATE TABLE IF NOT EXISTS order_lines (
    id              BIGSERIAL PRIMARY KEY,
    order_id        UUID        NOT NULL REFERENCES vendor_orders(id) ON DELETE CASCADE,
    description     TEXT        NOT NULL,
    quantity        INT         NOT NULL DEFAULT 1,
    unit_amount     BIGINT      NOT NULL DEFAULT 0,  -- minor units
    total_amount    BIGINT      NOT NULL DEFAULT 0,
    line_number     INT         NOT NULL,
    UNIQUE (order_id, line_number)
);

CREATE TABLE IF NOT EXISTS reviews (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id        UUID        NOT NULL REFERENCES vendor_orders(id),
    reviewer_id     UUID        NOT NULL REFERENCES users(id),
    rating          SMALLINT    NOT NULL CHECK (rating BETWEEN 1 AND 5),
    review_text     TEXT,
    visibility      TEXT        NOT NULL DEFAULT 'visible', -- 'visible','hidden','shown_with_disclaimer'
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_reviews_order    ON reviews(order_id);
CREATE INDEX IF NOT EXISTS idx_reviews_reviewer ON reviews(reviewer_id);
CREATE INDEX IF NOT EXISTS idx_reviews_visibility ON reviews(visibility);

CREATE TABLE IF NOT EXISTS review_attachments (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    review_id       UUID        NOT NULL REFERENCES reviews(id) ON DELETE CASCADE,
    file_path       TEXT        NOT NULL,
    original_name   TEXT        NOT NULL,
    content_type    TEXT        NOT NULL,  -- 'image/jpeg', 'image/png'
    size_bytes      BIGINT      NOT NULL,
    checksum        TEXT        NOT NULL,
    uploaded_by     UUID        NOT NULL REFERENCES users(id),
    uploaded_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS merchant_replies (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    review_id       UUID        NOT NULL REFERENCES reviews(id) ON DELETE CASCADE,
    order_id        UUID        NOT NULL REFERENCES vendor_orders(id),
    recorded_by     UUID        NOT NULL REFERENCES users(id),  -- Procurement Specialist
    reply_text      TEXT        NOT NULL,
    replied_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS negative_review_appeals (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    review_id       UUID        NOT NULL REFERENCES reviews(id),
    appealed_by     UUID        NOT NULL REFERENCES users(id),
    appeal_reason   TEXT        NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'pending', -- 'pending','under_review','decided'
    submitted_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at      TIMESTAMPTZ,
    decided_by      UUID        REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS appeal_evidence (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    appeal_id       UUID        NOT NULL REFERENCES negative_review_appeals(id) ON DELETE CASCADE,
    file_path       TEXT        NOT NULL,
    original_name   TEXT        NOT NULL,
    content_type    TEXT        NOT NULL,  -- 'application/pdf','image/jpeg','image/png'
    size_bytes      BIGINT      NOT NULL,
    checksum        TEXT        NOT NULL,
    uploaded_by     UUID        NOT NULL REFERENCES users(id),
    uploaded_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Sensitive metadata is encrypted at rest (Slice 3)
    encrypted_metadata TEXT
);

CREATE TABLE IF NOT EXISTS arbitration_outcomes (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    appeal_id       UUID        NOT NULL REFERENCES negative_review_appeals(id),
    decided_by      UUID        NOT NULL REFERENCES users(id),
    outcome         TEXT        NOT NULL, -- 'hide','show_with_disclaimer','restore'
    rationale       TEXT        NOT NULL,
    decided_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS review_visibility_events (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    review_id       UUID        NOT NULL REFERENCES reviews(id),
    changed_by      UUID        NOT NULL REFERENCES users(id),
    old_visibility  TEXT        NOT NULL,
    new_visibility  TEXT        NOT NULL,
    reason          TEXT,
    changed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Reconciliation and settlement
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS billing_rule_sets (
    id              BIGSERIAL PRIMARY KEY,
    name            TEXT        NOT NULL UNIQUE,
    description     TEXT,
    is_active       BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS billing_rule_versions (
    id              BIGSERIAL PRIMARY KEY,
    rule_set_id     BIGINT      NOT NULL REFERENCES billing_rule_sets(id),
    version_number  INT         NOT NULL,
    effective_from  DATE        NOT NULL,
    effective_to    DATE,
    vendor_scope    TEXT,
    warehouse_scope TEXT,
    transport_mode  TEXT,
    department_code TEXT,
    cost_center     TEXT,
    rule_definition JSONB       NOT NULL,  -- configurable billing parameters
    created_by      UUID        REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (rule_set_id, version_number)
);

CREATE TABLE IF NOT EXISTS statement_import_batches (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    imported_by     UUID        NOT NULL REFERENCES users(id),
    source_file     TEXT        NOT NULL,
    checksum        TEXT        NOT NULL,
    row_count       INT         NOT NULL DEFAULT 0,
    status          TEXT        NOT NULL DEFAULT 'pending', -- 'pending','processed','reconciled'
    imported_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS statement_rows (
    id              BIGSERIAL PRIMARY KEY,
    batch_id        UUID        NOT NULL REFERENCES statement_import_batches(id) ON DELETE CASCADE,
    order_id        UUID        REFERENCES vendor_orders(id),
    line_description TEXT       NOT NULL,
    statement_amount BIGINT     NOT NULL,   -- minor units
    currency        TEXT        NOT NULL DEFAULT 'USD',
    transaction_date DATE       NOT NULL,
    raw_data        JSONB
);

CREATE TABLE IF NOT EXISTS reconciliation_runs (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id        UUID        NOT NULL REFERENCES statement_import_batches(id),
    run_by          UUID        NOT NULL REFERENCES users(id),
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    status          TEXT        NOT NULL DEFAULT 'running', -- 'running','completed','failed'
    total_variances INT         NOT NULL DEFAULT 0,
    total_variance_amount BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS reconciliation_variances (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID        NOT NULL REFERENCES reconciliation_runs(id) ON DELETE CASCADE,
    statement_row_id BIGINT     NOT NULL REFERENCES statement_rows(id),
    order_id        UUID        REFERENCES vendor_orders(id),
    expected_amount BIGINT      NOT NULL,
    actual_amount   BIGINT      NOT NULL,
    variance_amount BIGINT      NOT NULL,  -- actual - expected (signed)
    rule_version_id BIGINT      REFERENCES billing_rule_versions(id),
    status          TEXT        NOT NULL DEFAULT 'open' -- 'open','writeoff_suggested','writeoff_approved','resolved'
);

CREATE TABLE IF NOT EXISTS variance_writeoff_suggestions (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    variance_id     UUID        NOT NULL REFERENCES reconciliation_variances(id),
    suggested_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    suggestion_reason TEXT,
    threshold_used  BIGINT,     -- the configured threshold in minor units
    approved_by     UUID        REFERENCES users(id),  -- Finance approval required
    approved_at     TIMESTAMPTZ,
    status          TEXT        NOT NULL DEFAULT 'pending' -- 'pending','approved','rejected'
);

CREATE TABLE IF NOT EXISTS settlement_batches (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID        NOT NULL REFERENCES reconciliation_runs(id),
    status          TEXT        NOT NULL DEFAULT 'draft', -- 'draft','under_review','approved','exported','settled','exception','voided'
    created_by      UUID        NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS settlement_lines (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id        UUID        NOT NULL REFERENCES settlement_batches(id) ON DELETE CASCADE,
    order_id        UUID        REFERENCES vendor_orders(id),
    amount          BIGINT      NOT NULL,
    currency        TEXT        NOT NULL DEFAULT 'USD',
    description     TEXT
);

CREATE TABLE IF NOT EXISTS cost_allocations (
    id              BIGSERIAL PRIMARY KEY,
    settlement_line_id UUID     NOT NULL REFERENCES settlement_lines(id) ON DELETE CASCADE,
    department_code TEXT        NOT NULL,
    cost_center     TEXT,
    allocated_amount BIGINT,    -- minor units (exclusive with allocated_pct)
    allocated_pct   NUMERIC(7,4), -- percentage (exclusive with allocated_amount)
    CONSTRAINT chk_allocation_method CHECK (
        (allocated_amount IS NOT NULL AND allocated_pct IS NULL) OR
        (allocated_amount IS NULL AND allocated_pct IS NOT NULL)
    )
);

CREATE TABLE IF NOT EXISTS ar_entries (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    settlement_batch_id UUID    NOT NULL REFERENCES settlement_batches(id),
    order_id        UUID        REFERENCES vendor_orders(id),
    amount          BIGINT      NOT NULL,
    currency        TEXT        NOT NULL DEFAULT 'USD',
    entry_date      DATE        NOT NULL DEFAULT CURRENT_DATE,
    description     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ap_entries (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    settlement_batch_id UUID    NOT NULL REFERENCES settlement_batches(id),
    order_id        UUID        REFERENCES vendor_orders(id),
    amount          BIGINT      NOT NULL,
    currency        TEXT        NOT NULL DEFAULT 'USD',
    entry_date      DATE        NOT NULL DEFAULT CURRENT_DATE,
    description     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS settlement_status_events (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id        UUID        NOT NULL REFERENCES settlement_batches(id),
    actor_id        UUID        NOT NULL REFERENCES users(id),
    old_status      TEXT        NOT NULL,
    new_status      TEXT        NOT NULL,
    reason          TEXT,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Export, jobs, and audit tables
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS export_jobs (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    export_type     TEXT        NOT NULL, -- 'csv_learning_record','finance_export','statement_export'
    status          TEXT        NOT NULL DEFAULT 'pending', -- 'pending','running','completed','failed'
    requested_by    UUID        NOT NULL REFERENCES users(id),
    requested_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    payload_hash    TEXT,
    schema_version  TEXT        NOT NULL DEFAULT '1',
    error_message   TEXT,
    attempt_count   INT         NOT NULL DEFAULT 0,
    max_attempts    INT         NOT NULL DEFAULT 3
);

CREATE TABLE IF NOT EXISTS export_deliveries (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id          UUID        NOT NULL REFERENCES export_jobs(id),
    delivery_type   TEXT        NOT NULL, -- 'file_drop','lan_webhook'
    destination     TEXT        NOT NULL, -- file path or webhook URL
    payload_hash    TEXT        NOT NULL,
    attempt_count   INT         NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMPTZ,
    final_status    TEXT,       -- 'delivered','failed','pending'
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS lan_webhook_targets (
    id              BIGSERIAL PRIMARY KEY,
    name            TEXT        NOT NULL UNIQUE,
    url             TEXT        NOT NULL,
    signing_secret_encrypted TEXT NOT NULL, -- encrypted at rest
    is_active       BOOLEAN     NOT NULL DEFAULT TRUE,
    event_types     TEXT[]      NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS webhook_delivery_attempts (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    delivery_id     UUID        NOT NULL REFERENCES export_deliveries(id),
    attempt_number  INT         NOT NULL,
    attempted_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    http_status     INT,
    response_body   TEXT,
    error_message   TEXT,
    succeeded       BOOLEAN     NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS scheduled_job_runs (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    job_type        TEXT        NOT NULL, -- 'search_rebuild','archive_refresh','export_retry','cleanup'
    trigger_source  TEXT        NOT NULL DEFAULT 'scheduler', -- 'scheduler','api','manual'
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    attempt_count   INT         NOT NULL DEFAULT 1,
    status          TEXT        NOT NULL DEFAULT 'running',
    duration_ms     BIGINT,
    error_summary   TEXT,
    compensation_action TEXT
);

CREATE TABLE IF NOT EXISTS job_retry_events (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID        NOT NULL REFERENCES scheduled_job_runs(id),
    retry_number    INT         NOT NULL,
    scheduled_at    TIMESTAMPTZ NOT NULL,
    reason          TEXT
);

CREATE TABLE IF NOT EXISTS compensation_events (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID        NOT NULL REFERENCES scheduled_job_runs(id),
    action          TEXT        NOT NULL,
    applied_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    result          TEXT
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id        UUID        REFERENCES users(id),
    action          TEXT        NOT NULL, -- structured action code
    category        TEXT        NOT NULL, -- 'auth','config','taxonomy','moderation','finance','exports'
    target_type     TEXT,
    target_id       TEXT,
    old_value       JSONB,
    new_value       JSONB,
    ip_address      TEXT,
    trace_id        TEXT,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_actor    ON audit_logs(actor_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_category ON audit_logs(category, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_action   ON audit_logs(action);

-- ─────────────────────────────────────────────────────────────────────────────
-- Configuration center
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS config_flags (
    id              BIGSERIAL PRIMARY KEY,
    flag_key        TEXT        NOT NULL UNIQUE,
    flag_value      BOOLEAN     NOT NULL DEFAULT FALSE,
    description     TEXT,
    updated_by      UUID        REFERENCES users(id),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS config_parameters (
    id              BIGSERIAL PRIMARY KEY,
    param_key       TEXT        NOT NULL UNIQUE,
    param_value     TEXT        NOT NULL,
    value_type      TEXT        NOT NULL DEFAULT 'string', -- 'string','integer','decimal','json'
    description     TEXT,
    min_value       TEXT,
    max_value       TEXT,
    updated_by      UUID        REFERENCES users(id),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS rollout_rules (
    id              BIGSERIAL PRIMARY KEY,
    flag_key        TEXT        NOT NULL REFERENCES config_flags(flag_key) ON DELETE CASCADE,
    role_name       TEXT        NOT NULL REFERENCES roles(name) ON DELETE CASCADE,
    is_enabled      BOOLEAN     NOT NULL DEFAULT FALSE,
    updated_by      UUID        REFERENCES users(id),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (flag_key, role_name)
);

CREATE TABLE IF NOT EXISTS client_version_rules (
    id              BIGSERIAL PRIMARY KEY,
    min_version     TEXT        NOT NULL,
    grace_until     TIMESTAMPTZ,  -- read-only grace until this timestamp
    is_blocked      BOOLEAN     NOT NULL DEFAULT FALSE,
    description     TEXT,
    created_by      UUID        REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
