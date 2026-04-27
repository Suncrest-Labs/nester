CREATE TABLE IF NOT EXISTS vault_transactions (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    vault_id   UUID        NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,
    type       TEXT        NOT NULL CHECK (type IN ('deposit', 'withdrawal')),
    amount     NUMERIC(28, 8) NOT NULL CHECK (amount > 0),
    tx_hash    TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_vault_transactions_vault_id ON vault_transactions (vault_id);
