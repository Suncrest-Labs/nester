DROP INDEX IF EXISTS idx_savings_goals_user_created;
DROP INDEX IF EXISTS idx_savings_goals_user_category;
DROP INDEX IF EXISTS idx_savings_goals_search_vector;
ALTER TABLE savings_goals DROP COLUMN IF EXISTS search_vector;
