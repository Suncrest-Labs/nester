DROP INDEX IF EXISTS idx_savings_goals_vault_id;

ALTER TABLE savings_goals DROP COLUMN IF EXISTS vault_id;
