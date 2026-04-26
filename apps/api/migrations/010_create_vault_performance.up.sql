CREATE TABLE IF NOT EXISTS vault_performance_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vault_id UUID NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,
    total_balance NUMERIC(20, 8) NOT NULL,
    total_deposited NUMERIC(20, 8) NOT NULL,
    total_yield_earned NUMERIC(20, 8) NOT NULL,
    share_price NUMERIC(20, 8) NOT NULL,
    snapshot_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    allocation_breakdown JSONB,
    CONSTRAINT unique_vault_snapshot UNIQUE (vault_id, snapshot_at)
);

CREATE INDEX IF NOT EXISTS idx_snapshots_vault_time
    ON vault_performance_snapshots(vault_id, snapshot_at DESC);

CREATE TABLE IF NOT EXISTS apy_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vault_id UUID NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,
    period VARCHAR(10) NOT NULL,
    realized_apy NUMERIC(8, 4) NOT NULL,
    calculated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_apy_history_vault_period
    ON apy_history(vault_id, period, calculated_at DESC);
