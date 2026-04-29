-- Schema (mirrors apps/api/migrations in order)

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY,
    wallet_address TEXT NOT NULL UNIQUE CHECK (length(btrim(wallet_address)) > 0),
    display_name TEXT NOT NULL,
    kyc_status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

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

CREATE TABLE IF NOT EXISTS allocations (
    id UUID PRIMARY KEY,
    vault_id UUID NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,
    protocol TEXT NOT NULL CHECK (char_length(protocol) > 0),
    amount NUMERIC(20,8) NOT NULL CHECK (amount >= 0),
    apy NUMERIC(10,4) NOT NULL CHECK (apy >= 0),
    allocated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_allocations_vault_id ON allocations (vault_id);

CREATE TABLE IF NOT EXISTS settlements (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    vault_id UUID NOT NULL REFERENCES vaults(id) ON DELETE RESTRICT,
    amount NUMERIC(20,8) NOT NULL CHECK (amount > 0),
    currency VARCHAR(10) NOT NULL,
    fiat_currency VARCHAR(10) NOT NULL,
    fiat_amount NUMERIC(20,8) NOT NULL CHECK (fiat_amount > 0),
    exchange_rate NUMERIC(20,8) NOT NULL CHECK (exchange_rate > 0),
    destination_type VARCHAR(50) NOT NULL,
    destination_provider VARCHAR(50) NOT NULL,
    destination_account_number VARCHAR(100) NOT NULL,
    destination_account_name VARCHAR(200) NOT NULL,
    destination_bank_code VARCHAR(20) NOT NULL DEFAULT '',
    status VARCHAR(30) NOT NULL DEFAULT 'initiated'
        CHECK (status IN (
            'initiated',
            'liquidity_matched',
            'fiat_dispatched',
            'confirmed',
            'failed'
        )),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_settlements_user_id ON settlements(user_id);
CREATE INDEX IF NOT EXISTS idx_settlements_vault_id ON settlements(vault_id);
CREATE INDEX IF NOT EXISTS idx_settlements_status ON settlements(status);

-- Seed data

INSERT INTO users (id, wallet_address, display_name, kyc_status, created_at, updated_at) VALUES
    ('550e8400-e29b-41d4-a716-446655440001', 'GDUMMYWALLET000000000000000000000000000000000000000000001', 'Test User', 'approved', NOW(), NOW());

INSERT INTO vaults (id, user_id, contract_address, total_deposited, current_balance, currency, status) VALUES
    ('550e8400-e29b-41d4-a716-446655440010',
     '550e8400-e29b-41d4-a716-446655440001',
     'CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCFW3',
     10000.00, 10234.56, 'USDC', 'active'),
    ('550e8400-e29b-41d4-a716-446655440011',
     '550e8400-e29b-41d4-a716-446655440001',
     'CCLQBFQKIIASLN7MXDQFAUXHQXPKR5ZVGKIMKNBZMKWL4LNKQXQXQAB',
     5000.00, 5150.25, 'USDC', 'active');

INSERT INTO allocations (id, vault_id, protocol, amount, apy, allocated_at) VALUES
    ('550e8400-e29b-41d4-a716-446655440020',
     '550e8400-e29b-41d4-a716-446655440010',
     'Blend', 6000.00, 8.5000, NOW()),
    ('550e8400-e29b-41d4-a716-446655440021',
     '550e8400-e29b-41d4-a716-446655440010',
     'Aave', 4000.00, 7.2500, NOW()),
    ('550e8400-e29b-41d4-a716-446655440022',
     '550e8400-e29b-41d4-a716-446655440011',
     'Compound', 5000.00, 6.8000, NOW());

INSERT INTO settlements (
    id, user_id, vault_id, amount, currency, fiat_currency, fiat_amount,
    exchange_rate, destination_type, destination_provider,
    destination_account_number, destination_account_name, destination_bank_code,
    status, created_at, completed_at
) VALUES
    ('550e8400-e29b-41d4-a716-446655440030',
     '550e8400-e29b-41d4-a716-446655440001',
     '550e8400-e29b-41d4-a716-446655440010',
     100.00, 'USDC', 'NGN', 165000.00, 1650.00,
     'bank_transfer', 'paystack', '0123456789', 'Test User', '044',
     'confirmed',
     NOW() - INTERVAL '2 days',
     NOW() - INTERVAL '2 days' + INTERVAL '10 seconds'),
    ('550e8400-e29b-41d4-a716-446655440031',
     '550e8400-e29b-41d4-a716-446655440001',
     '550e8400-e29b-41d4-a716-446655440010',
     50.00, 'USDC', 'NGN', 82500.00, 1650.00,
     'bank_transfer', 'paystack', '0123456789', 'Test User', '044',
     'initiated',
     NOW(), NULL),
    ('550e8400-e29b-41d4-a716-446655440032',
     '550e8400-e29b-41d4-a716-446655440001',
     '550e8400-e29b-41d4-a716-446655440011',
     200.00, 'USDC', 'NGN', 330000.00, 1650.00,
     'bank_transfer', 'paystack', '9876543210', 'Test User', '058',
     'fiat_dispatched',
     NOW() - INTERVAL '1 hour', NULL);
