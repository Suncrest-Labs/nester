-- Vault-level soft cap (nester#1316): the maximum current_balance a vault
-- may reach. NULL means no limit. Persists the domain field vault.SoftCapacity,
-- which previously existed only in memory and was never enforced.
ALTER TABLE vaults ADD COLUMN IF NOT EXISTS soft_capacity NUMERIC(20,8) CHECK (soft_capacity IS NULL OR soft_capacity > 0);

-- Per-user rolling 24h deposit cap (nester#1316): the maximum a single user
-- may deposit across all vaults in any trailing 24h window. NULL means no
-- limit. Bounds exposure from a single compromised account independent of
-- any one vault's own cap.
ALTER TABLE users ADD COLUMN IF NOT EXISTS daily_deposit_cap NUMERIC(20,8) CHECK (daily_deposit_cap IS NULL OR daily_deposit_cap > 0);

-- Supports the rolling-24h sum query (user_id, type='deposit', created_at >= now() - 24h)
-- the per-user cap check runs on every deposit.
CREATE INDEX IF NOT EXISTS idx_vault_transactions_user_deposits_at
    ON vault_transactions (user_id, created_at)
    WHERE type = 'deposit';
