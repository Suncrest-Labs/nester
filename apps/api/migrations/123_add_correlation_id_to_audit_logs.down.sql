DROP INDEX IF EXISTS idx_audit_logs_correlation_id;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS correlation_id;
