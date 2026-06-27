ALTER TABLE savings_goals DROP COLUMN IF EXISTS completion_action;
ALTER TABLE savings_goals DROP COLUMN IF EXISTS completed_at;
ALTER TABLE savings_goals DROP COLUMN IF EXISTS status;

DROP INDEX IF EXISTS idx_savings_goals_user_status;
