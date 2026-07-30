-- Index used by the rotation tool to find rows not yet on the active key
-- version (WHERE key_version <> $active). Built CONCURRENTLY so it does not
-- take a write lock on bank_accounts during deploy. This statement must run
-- outside a transaction; keep it the sole statement in this migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_bank_accounts_key_version
    ON bank_accounts (key_version);
