-- Ledger reconciliation records: stores results of periodic checks against on-chain state.
-- Does NOT auto-correct; only alerts on drift beyond tolerance.

CREATE TABLE IF NOT EXISTS ledger_reconciliation_records (
    id                        UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    vault_id                  UUID        NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,
    ledger_vault_pool_balance BIGINT      NOT NULL, -- ledger's vault_asset_pool balance, stroops
    on_chain_balance          BIGINT      NOT NULL, -- on-chain total_assets, stroops
    difference                BIGINT      NOT NULL, -- ledger - on_chain
    tolerance                 BIGINT      NOT NULL, -- allowed drift in stroops
    status                    TEXT        NOT NULL CHECK (status IN ('ok','drift','error')),
    details                   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ledger_reconciliation_vault_created
    ON ledger_reconciliation_records (vault_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_ledger_reconciliation_status_created
    ON ledger_reconciliation_records (status, created_at DESC);

-- Balance verification mismatches: records cache vs recomputed mismatches
CREATE TABLE IF NOT EXISTS ledger_balance_verifications (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id  UUID        NOT NULL REFERENCES ledger_accounts(id) ON DELETE CASCADE,
    cached      BIGINT      NOT NULL,
    computed    BIGINT      NOT NULL,
    difference  BIGINT      NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ledger_balance_verifications_created
    ON ledger_balance_verifications (created_at DESC);
