-- Drop CONCURRENTLY to avoid a write lock; must be the sole statement and run
-- outside a transaction.
DROP INDEX CONCURRENTLY IF EXISTS idx_kyc_documents_key_version;
