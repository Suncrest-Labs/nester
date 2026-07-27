-- Drop CONCURRENTLY to avoid a write lock; must be the sole statement and run
-- outside a transaction.
DROP INDEX CONCURRENTLY IF EXISTS idx_bank_accounts_key_version;
