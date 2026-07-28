-- Per-leg realised-slippage detail for the slippage-safe rebalance
-- plan/execute split (issue #810). Extends `vault_rebalances` (migration
-- 030) with one row per executed leg, so tolerance settings can be tuned
-- against evidence rather than intuition.
CREATE TABLE IF NOT EXISTS vault_rebalance_legs (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rebalance_id           UUID REFERENCES vault_rebalances(id) ON DELETE CASCADE,
    vault_contract_address TEXT NOT NULL CHECK (char_length(vault_contract_address) > 0),
    plan_hash              TEXT,
    source_id              TEXT NOT NULL CHECK (char_length(source_id) > 0),
    delta                  NUMERIC(38,0) NOT NULL,
    amount_out             NUMERIC(38,0) NOT NULL DEFAULT 0,
    min_out                NUMERIC(38,0) NOT NULL DEFAULT 0,
    occurred_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_vault_rebalance_legs_vault ON vault_rebalance_legs (vault_contract_address, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_vault_rebalance_legs_rebalance_id ON vault_rebalance_legs (rebalance_id);
