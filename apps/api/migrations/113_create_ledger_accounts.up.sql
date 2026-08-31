-- Ledger accounts: the chart of accounts for double-entry bookkeeping.
-- Every value movement is recorded as balanced postings between these accounts.
-- Amounts are ALWAYS stored as integers in the smallest unit (stroops, 7 decimals for USDC).
-- Conversion to human-readable decimals happens ONLY at the presentation edge.
--
-- Account types:
-- - user_vault_position: a user's vault position (vault_id + user_id)
-- - vault_asset_pool: the vault's asset pool (vault_id)
-- - fee_account: performance fees collected
-- - penalty_escrow: early-withdraw penalty escrow (vault_id)
-- - treasury: protocol treasury
-- - yield_source: per-adapter yield source (adapter_name) — tracks yield per protocol
-- - system_suspense: global suspense for balancing (e.g., external deposits)
-- - external: optional external world account

CREATE TABLE IF NOT EXISTS ledger_accounts (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    account_type  TEXT        NOT NULL CHECK (account_type IN (
        'user_vault_position',
        'vault_asset_pool',
        'fee_account',
        'penalty_escrow',
        'treasury',
        'yield_source',
        'system_suspense',
        'external'
    )),
    vault_id      UUID        REFERENCES vaults(id) ON DELETE RESTRICT,
    user_id       UUID        REFERENCES users(id) ON DELETE RESTRICT,
    adapter_name  TEXT, -- protocol name for yield_source, e.g. 'blend', 'aave'
    asset_code    TEXT        NOT NULL DEFAULT 'USDC', -- asset identifier, e.g. USDC
    asset_unit    TEXT        NOT NULL DEFAULT 'stroops', -- smallest integer unit, document unit
    description   TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Document the unit for every asset: stroops (7 decimals) is the base unit, never floats in ledger.
COMMENT ON COLUMN ledger_accounts.asset_unit IS 'Base unit for integer amounts, e.g. stroops (1 USDC = 10^7 stroops). Never floats.';
COMMENT ON COLUMN ledger_accounts.asset_code IS 'Asset code, e.g. USDC, XLM. Amounts are in asset_unit.';

-- Unique constraints per account type to make get-or-create idempotent.
CREATE UNIQUE INDEX IF NOT EXISTS uq_ledger_accounts_user_position
    ON ledger_accounts (account_type, vault_id, user_id, asset_code)
    WHERE account_type = 'user_vault_position';

CREATE UNIQUE INDEX IF NOT EXISTS uq_ledger_accounts_vault_pool
    ON ledger_accounts (account_type, vault_id, asset_code)
    WHERE account_type = 'vault_asset_pool';

CREATE UNIQUE INDEX IF NOT EXISTS uq_ledger_accounts_fee
    ON ledger_accounts (account_type, asset_code)
    WHERE account_type = 'fee_account';

CREATE UNIQUE INDEX IF NOT EXISTS uq_ledger_accounts_penalty
    ON ledger_accounts (account_type, vault_id, asset_code)
    WHERE account_type = 'penalty_escrow';

CREATE UNIQUE INDEX IF NOT EXISTS uq_ledger_accounts_treasury
    ON ledger_accounts (account_type, asset_code)
    WHERE account_type = 'treasury';

CREATE UNIQUE INDEX IF NOT EXISTS uq_ledger_accounts_yield_source
    ON ledger_accounts (account_type, adapter_name, asset_code)
    WHERE account_type = 'yield_source';

CREATE UNIQUE INDEX IF NOT EXISTS uq_ledger_accounts_suspense
    ON ledger_accounts (account_type, asset_code)
    WHERE account_type = 'system_suspense';

CREATE UNIQUE INDEX IF NOT EXISTS uq_ledger_accounts_external
    ON ledger_accounts (account_type, asset_code)
    WHERE account_type = 'external';

-- Helpful indexes for lookups
CREATE INDEX IF NOT EXISTS idx_ledger_accounts_vault_id ON ledger_accounts (vault_id) WHERE vault_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_ledger_accounts_user_id ON ledger_accounts (user_id) WHERE user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_ledger_accounts_type ON ledger_accounts (account_type);
CREATE INDEX IF NOT EXISTS idx_ledger_accounts_adapter ON ledger_accounts (adapter_name) WHERE adapter_name IS NOT NULL;
