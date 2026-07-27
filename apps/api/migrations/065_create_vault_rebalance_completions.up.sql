-- Summary row for a completed slippage-safe rebalance execution (issue
-- #810). Decoupled from `vault_rebalance_legs` the same way
-- `penalty_distributions` is decoupled from `penalty_events` (migration
-- 063) — the on-chain `rebalance_completed` event carries no foreign key
-- back to individual legs, so correlating them precisely off-chain isn't
-- possible without a shared session id the contract doesn't emit. Each
-- table is independently reconstructable from its own event stream.
CREATE TABLE IF NOT EXISTS vault_rebalance_completions (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vault_contract_address TEXT NOT NULL CHECK (char_length(vault_contract_address) > 0),
    plan_hash              TEXT NOT NULL,
    total_value_moved      NUMERIC(38,0) NOT NULL DEFAULT 0 CHECK (total_value_moved >= 0),
    realized_slippage_bps  INTEGER NOT NULL DEFAULT 0 CHECK (realized_slippage_bps >= 0),
    occurred_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_vault_rebalance_completions_vault
    ON vault_rebalance_completions (vault_contract_address, occurred_at DESC);
