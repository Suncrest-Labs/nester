DROP INDEX IF EXISTS idx_savings_goals_share_token;
ALTER TABLE savings_goals
    DROP COLUMN IF EXISTS share_enabled_at,
    DROP COLUMN IF EXISTS share_token;
