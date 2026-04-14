-- migrations/003_recommendations.sql
-- Adds session tracking columns to recommendation tables for Slice 6.

ALTER TABLE behavior_events
    ADD COLUMN IF NOT EXISTS session_id TEXT;

ALTER TABLE recommendation_impressions
    ADD COLUMN IF NOT EXISTS session_id TEXT;

CREATE INDEX IF NOT EXISTS idx_behavior_session ON behavior_events(session_id)
    WHERE session_id IS NOT NULL;
