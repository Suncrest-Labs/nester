-- Schema (mirrors apps/api/migrations in order)

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY,
    wallet_address TEXT NOT NULL UNIQUE CHECK (length(btrim(wallet_address)) > 0),
    display_name TEXT NOT NULL,
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

CREATE TABLE IF NOT EXISTS user_roles (
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       VARCHAR(50) NOT NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    granted_by UUID        REFERENCES users(id),
    PRIMARY KEY (user_id, role)
);

CREATE TABLE IF NOT EXISTS system_state (
    key        TEXT        PRIMARY KEY,
    value      TEXT        NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed the event-indexer cursor.
INSERT INTO system_state (key, value)
VALUES ('event_indexer.last_ledger', '0')
ON CONFLICT (key) DO NOTHING;

-- Seed data

INSERT INTO users (id, wallet_address, display_name, created_at, updated_at) VALUES
    ('550e8400-e29b-41d4-a716-446655440001', 'GBDZVKPNWE5K3VQXXS3F2XW56XG6Y74NXZ4L6R445VMBG6X5D74NXR7Z', 'Test User', NOW(), NOW())
ON CONFLICT DO NOTHING;

INSERT INTO user_roles (user_id, role, granted_at, granted_by) VALUES
    ('550e8400-e29b-41d4-a716-446655440001', 'admin', NOW(), NULL)
ON CONFLICT DO NOTHING;

-- Contract addresses are the real testnet deployments from
-- packages/contracts/scripts/deployed-testnet.env, not placeholders. The event
-- indexer builds its RPC event filter from these rows, and a fabricated ID
-- fails the whole poll ("contract ID 1 invalid") rather than just that vault:
-- the previous XLM value was 55 characters and could not be decoded at all, so
-- no deposit was ever indexed and positions never reached the database.
INSERT INTO vaults (id, user_id, contract_address, total_deposited, current_balance, currency, status) VALUES
    ('550e8400-e29b-41d4-a716-446655440010',
     '550e8400-e29b-41d4-a716-446655440001',
     'CBYJXQUCJ475OREU4TQGGPYFC4XX2EW7FR5XNNR5X2MH3GQJLOTIT5YL',
     10000.00, 10234.56, 'USDC', 'active'),
    ('550e8400-e29b-41d4-a716-446655440011',
     '550e8400-e29b-41d4-a716-446655440001',
     'CAQUVMTUGONBIUUXKUP3ANIOXBVLSQNXOEP2P5AWUJIM3XMH3NADZDKR',
     5000.00, 5150.25, 'XLM', 'active')
ON CONFLICT (id) DO NOTHING;

INSERT INTO allocations (id, vault_id, protocol, amount, apy, allocated_at) VALUES
    ('550e8400-e29b-41d4-a716-446655440020',
     '550e8400-e29b-41d4-a716-446655440010',
     'Blend', 6000.00, 8.5000, NOW()),
    ('550e8400-e29b-41d4-a716-446655440021',
     '550e8400-e29b-41d4-a716-446655440010',
     'Aave', 4000.00, 7.2500, NOW()),
    ('550e8400-e29b-41d4-a716-446655440022',
     '550e8400-e29b-41d4-a716-446655440011',
     'Compound', 5000.00, 6.8000, NOW())
ON CONFLICT (id) DO NOTHING;

