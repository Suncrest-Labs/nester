CREATE TABLE IF NOT EXISTS vault_tvl_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vault_id UUID NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,
    tvl_usdc NUMERIC(20, 6) NOT NULL,
    total_depositors INTEGER NOT NULL DEFAULT 0,
    snapshot_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_vault_tvl_snapshots_vault_time
    ON vault_tvl_snapshots(vault_id, snapshot_at DESC);
