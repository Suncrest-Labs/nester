CREATE TABLE IF NOT EXISTS vaults (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    contract_address TEXT NOT NULL CHECK (char_length(contract_address) > 0),
    total_deposited NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (total_deposited >= 0),
    current_balance NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (current_balance >= 0),
    currency TEXT NOT NULL CHECK (char_length(currency) > 0),
    status TEXT NOT NULL CHECK (status IN ('active', 'paused', 'closed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_vaults_user_id ON vaults (user_id);
