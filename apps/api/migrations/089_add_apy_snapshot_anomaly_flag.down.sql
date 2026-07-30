DROP INDEX IF EXISTS idx_apy_snapshots_flagged;
ALTER TABLE apy_snapshots DROP COLUMN IF EXISTS flag_reason;
ALTER TABLE apy_snapshots DROP COLUMN IF EXISTS flagged;
