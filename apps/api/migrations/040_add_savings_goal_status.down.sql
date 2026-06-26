DROP INDEX IF EXISTS ix_savings_goals_status;
ALTER TABLE savings_goals DROP COLUMN IF EXISTS completed_at;
ALTER TABLE savings_goals DROP COLUMN IF EXISTS status;
