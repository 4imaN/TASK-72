-- Migration 011 — scrub plaintext evidence metadata at rest.
--
-- Prior code wrote original_name, content_type, and size_bytes into
-- appeal_evidence even when encrypted_metadata was populated, creating a
-- plaintext copy of data that the encryption-at-rest model was supposed to
-- protect. This migration NULLs those columns for every row that already
-- has a ciphertext, making encrypted_metadata the sole source of truth.

UPDATE appeal_evidence
SET original_name = NULL,
    content_type  = NULL,
    size_bytes    = NULL
WHERE encrypted_metadata IS NOT NULL
  AND encrypted_metadata <> '';
