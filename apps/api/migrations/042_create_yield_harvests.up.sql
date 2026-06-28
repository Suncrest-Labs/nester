CREATE TABLE IF NOT EXISTS yield_harvests (
    id           UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID           NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    vault_id     UUID           NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,
    amount       NUMERIC(28, 8) NOT NULL CHECK (amount > 0),
    currency     TEXT           NOT NULL,
    harvested_at TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    tx_hash      TEXT
);

CREATE INDEX IF NOT EXISTS idx_yield_harvests_user_id     ON yield_harvests (user_id);
CREATE INDEX IF NOT EXISTS idx_yield_harvests_vault_id    ON yield_harvests (vault_id);
CREATE INDEX IF NOT EXISTS idx_yield_harvests_harvested_at ON yield_harvests (harvested_at DESC);
