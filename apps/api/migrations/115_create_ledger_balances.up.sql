-- Ledger balances: materialised cache of sum(entries) per account for scale.
-- Updated within the same transaction as ledger_entries, and verified by a periodic job
-- that recomputes from raw entries and asserts equality. This makes the optimisation safe.

CREATE TABLE IF NOT EXISTS ledger_balances (
    account_id UUID        PRIMARY KEY REFERENCES ledger_accounts(id) ON DELETE CASCADE,
    balance    BIGINT      NOT NULL DEFAULT 0, -- sum of ledger_entries.amount for this account, stroops integer
    asset_code TEXT        NOT NULL DEFAULT 'USDC',
    asset_unit TEXT        NOT NULL DEFAULT 'stroops',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version    BIGINT      NOT NULL DEFAULT 0
);

COMMENT ON COLUMN ledger_balances.balance IS 'Cached sum of ledger_entries for account_id. Must equal SUM(amount) from ledger_entries. Verified by periodic job.';
COMMENT ON COLUMN ledger_balances.asset_unit IS 'Base unit: stroops, integer discipline.';

CREATE INDEX IF NOT EXISTS idx_ledger_balances_updated_at ON ledger_balances (updated_at);
CREATE INDEX IF NOT EXISTS idx_ledger_balances_asset_code ON ledger_balances (asset_code);
