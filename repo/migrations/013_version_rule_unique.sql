-- 013_version_rule_unique.sql
-- Adds a UNIQUE constraint on client_version_rules.min_version so the
-- store's ON CONFLICT (min_version) upsert works. Without this the
-- PUT /admin/config/version-rules handler returns 500 because Postgres
-- rejects the ON CONFLICT clause with "no unique or exclusion constraint
-- matching the ON CONFLICT specification".
--
-- Safe against existing duplicate rows: if any exist, we keep only the most
-- recently created row per min_version before adding the constraint.

BEGIN;

DELETE FROM client_version_rules a
USING client_version_rules b
WHERE a.min_version = b.min_version
  AND a.id < b.id;

ALTER TABLE client_version_rules
    ADD CONSTRAINT client_version_rules_min_version_key UNIQUE (min_version);

COMMIT;
