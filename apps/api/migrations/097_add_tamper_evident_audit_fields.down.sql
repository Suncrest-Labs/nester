DROP INDEX IF EXISTS idx_audit_logs_entry_hash;
DROP INDEX IF EXISTS idx_audit_logs_sequence;

ALTER TABLE audit_logs DROP COLUMN IF EXISTS redacted;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS anchor_tx_hash;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS anchored;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS entry_hash;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS prev_hash;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS detail_hash;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS detail;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS target;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS actor;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS sequence;
