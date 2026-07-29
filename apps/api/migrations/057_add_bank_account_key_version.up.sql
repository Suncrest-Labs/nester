-- Records which encryption key version sealed account_number_encrypted so keys
-- can be rotated without rewriting historical rows. Rows created before key
-- versioning were sealed with the original (v1) key, so the column defaults to
-- 'v1' and existing data keeps decrypting after this migration is applied.
--
-- The supporting index is created separately (migration 058) with CREATE INDEX
-- CONCURRENTLY so a large bank_accounts table is not write-locked during deploy.
ALTER TABLE bank_accounts
    ADD COLUMN key_version VARCHAR(32) NOT NULL DEFAULT 'v1';
