-- migrations/004_reviews_moderation.sql
-- Adds moderation_queue table and missing columns for Slice 7.

-- Add disclaimer_text column to reviews for arbitration outcomes
ALTER TABLE reviews ADD COLUMN IF NOT EXISTS disclaimer_text TEXT;

-- Moderation queue: items flagged for review
CREATE TABLE IF NOT EXISTS moderation_queue (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    review_id       UUID        NOT NULL REFERENCES reviews(id) ON DELETE CASCADE,
    reason          TEXT        NOT NULL,
    flagged_by      UUID        NOT NULL REFERENCES users(id),
    status          TEXT        NOT NULL DEFAULT 'pending', -- 'pending','approved','rejected','escalated'
    moderator_id    UUID        REFERENCES users(id),
    decision_notes  TEXT,
    flagged_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_moderation_queue_status    ON moderation_queue(status);
CREATE INDEX IF NOT EXISTS idx_moderation_queue_review_id ON moderation_queue(review_id);
