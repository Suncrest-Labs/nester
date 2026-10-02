DROP INDEX IF EXISTS idx_vault_transactions_user_deposits_at;
ALTER TABLE users DROP COLUMN IF EXISTS daily_deposit_cap;
ALTER TABLE vaults DROP COLUMN IF EXISTS soft_capacity;
