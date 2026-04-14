-- Migration 012 — allow NULLs in appeal_evidence metadata columns.
--
-- The encrypted-only write path (appeals_store.go CreateAppeal) passes NULL
-- for original_name, content_type, and size_bytes when the encryptor is
-- available, relying on encrypted_metadata as the sole source of truth at
-- rest. The base schema (001_init.sql) defined these columns as NOT NULL,
-- which makes the encrypted path and the scrub migration (011) statically
-- inconsistent. This migration drops the NOT NULL constraints so both paths
-- are coherent.

ALTER TABLE appeal_evidence
  ALTER COLUMN original_name DROP NOT NULL,
  ALTER COLUMN content_type  DROP NOT NULL,
  ALTER COLUMN size_bytes    DROP NOT NULL;
