DROP INDEX IF EXISTS uq_jobs_idempotency_key;
DROP INDEX IF EXISTS idx_jobs_type_status;
DROP INDEX IF EXISTS idx_jobs_running_lease;
DROP INDEX IF EXISTS idx_jobs_pending_ready;
DROP TABLE IF EXISTS jobs;
