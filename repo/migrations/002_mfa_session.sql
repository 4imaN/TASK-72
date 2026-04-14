-- migrations/002_mfa_session.sql
-- Track whether the MFA challenge was completed for a session.
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS mfa_verified BOOLEAN NOT NULL DEFAULT FALSE;
