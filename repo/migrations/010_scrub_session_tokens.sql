-- Migration 010 — scrub leaked session cookie tokens from analytics data.
--
-- Prior code stored the raw portal_session cookie value in
-- behavior_events.session_id and recommendation_impressions.session_id.
-- Those are credentials, not analytics identifiers. This migration NULLs out
-- any value that looks like a hex token (64+ chars) so the credential is no
-- longer accessible from analytics queries.

UPDATE behavior_events
SET session_id = NULL
WHERE session_id IS NOT NULL AND length(session_id) >= 64;

UPDATE recommendation_impressions
SET session_id = NULL
WHERE session_id IS NOT NULL AND length(session_id) >= 64;
