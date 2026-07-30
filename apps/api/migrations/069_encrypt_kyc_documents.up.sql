-- Adds encrypted columns and a blind-index fingerprint for KYC document fields
-- that contain sensitive PII (government ID number, S3 object keys pointing to
-- ID document scans). Encrypted columns sit alongside the existing plaintext
-- columns so the migration is safe and rollbackable.
--
--  1. The plaintext columns (id_number, front_object_key, back_object_key)
--     remain until migration 071 drops them after the backfill is verified.
--  2. The key_version column mirrors the bank_accounts pattern (migration 057)
--     and enables non-destructive key rotation.
--  3. The id_number_fingerprint column is a blind index for exact-match
--     deduplication without decryption.
--
-- The supporting CONCURRENTLY index is created in migration 070 to avoid
-- write-locking the table during deploy.
ALTER TABLE kyc_documents
    ADD COLUMN id_number_encrypted         BYTEA,
    ADD COLUMN id_number_fingerprint       TEXT,
    ADD COLUMN front_object_key_encrypted  BYTEA,
    ADD COLUMN back_object_key_encrypted   BYTEA,
    ADD COLUMN key_version                 VARCHAR(32) NOT NULL DEFAULT 'v1';
