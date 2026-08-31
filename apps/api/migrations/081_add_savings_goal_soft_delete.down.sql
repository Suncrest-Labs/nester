DROP INDEX IF EXISTS idx_savings_goals_deleted_at;

ALTER TABLE savings_goals
    DROP COLUMN IF EXISTS deleted_at;
