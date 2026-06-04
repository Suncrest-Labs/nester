CREATE TABLE IF NOT EXISTS vault_rebalances (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vault_id UUID NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,
    strategy TEXT NOT NULL CHECK (char_length(strategy) > 0),
    dry_run BOOLEAN NOT NULL DEFAULT false,
    status TEXT NOT NULL CHECK (status IN ('pending', 'submitted', 'completed', 'failed', 'dry_run')),
    tx_hash TEXT,
    projected_deltas JSONB,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_vault_rebalances_vault_id ON vault_rebalances (vault_id);
CREATE INDEX IF NOT EXISTS idx_vault_rebalances_created_at ON vault_rebalances (created_at DESC);

-- Prevent concurrent in-flight rebalances for the same vault.
CREATE UNIQUE INDEX IF NOT EXISTS idx_vault_rebalances_in_flight
    ON vault_rebalances (vault_id)
    WHERE status IN ('pending', 'submitted');
