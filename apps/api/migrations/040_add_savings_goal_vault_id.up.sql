ALTER TABLE savings_goals
    ADD COLUMN IF NOT EXISTS vault_id UUID NULL REFERENCES vaults(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_savings_goals_vault_id ON savings_goals(vault_id);
