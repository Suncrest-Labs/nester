DROP INDEX IF EXISTS idx_savings_goals_onchain_goal_id;

ALTER TABLE savings_goals
    DROP COLUMN IF EXISTS onchain_goal_id,
    DROP COLUMN IF EXISTS onchain_status;
