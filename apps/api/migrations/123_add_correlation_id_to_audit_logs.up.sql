-- Correlation id for audit_logs rows (nester#1339): background-job-written
-- entries (data retention sweeps, APY-drift rebalance triggers) had no way
-- to carry the id of the job run that produced them, unlike request-triggered
-- entries which already have request_id available via pkg/logger. NULL for
-- any existing row and for any future entry a caller doesn't set.
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS correlation_id TEXT;

-- Supports tracing every entry written by one job run or HTTP request back
-- to it in one query.
CREATE INDEX IF NOT EXISTS idx_audit_logs_correlation_id ON audit_logs(correlation_id) WHERE correlation_id IS NOT NULL;
