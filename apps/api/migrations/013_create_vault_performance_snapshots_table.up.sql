CREATE TABLE IF NOT EXISTS vault_performance_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vault_id UUID NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,
    total_assets NUMERIC(28, 8) NOT NULL,
    total_shares NUMERIC(28, 8) NOT NULL,
    share_price NUMERIC(28, 8) NOT NULL,
    yield_earned NUMERIC(28, 8) NOT NULL DEFAULT 0,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_vault_performance_snapshots_vault_id_time ON vault_performance_snapshots(vault_id, timestamp DESC);
