CREATE TABLE IF NOT EXISTS transactions (
    id UUID PRIMARY KEY,
    vault_id UUID NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN ('deposit', 'withdrawal', 'allocation', 'settlement')),
    amount NUMERIC(20,8) NOT NULL CHECK (amount > 0),
    currency TEXT NOT NULL CHECK (char_length(currency) > 0),
    tx_hash TEXT UNIQUE,
    status TEXT NOT NULL CHECK (status IN ('pending', 'completed', 'failed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_transactions_vault_id ON transactions (vault_id);
