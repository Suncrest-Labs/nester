-- Early-exit penalty escrow events (issue #805).
--
-- `penalty_events` records every `penalty_charged` emission so protocol
-- revenue is reconstructable off-chain purely from events. `penalty_distributions`
-- records each `distribute_penalties` sweep (depositor/treasury split, plus the
-- dust retained in the escrow for the next round).
CREATE TABLE IF NOT EXISTS penalty_events (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vault_contract_address TEXT NOT NULL CHECK (char_length(vault_contract_address) > 0),
    user_address           TEXT NOT NULL CHECK (char_length(user_address) > 0),
    amount                 NUMERIC(38,0) NOT NULL CHECK (amount > 0),
    shares_burned          NUMERIC(38,0) NOT NULL DEFAULT 0 CHECK (shares_burned >= 0),
    reason                 TEXT NOT NULL CHECK (reason IN ('early_withdrawal', 'lock_break', 'emergency_exit', 'weight_deviation')),
    occurred_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_penalty_events_vault ON penalty_events (vault_contract_address, occurred_at DESC);

CREATE TABLE IF NOT EXISTS penalty_distributions (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vault_contract_address TEXT NOT NULL CHECK (char_length(vault_contract_address) > 0),
    depositor_amount       NUMERIC(38,0) NOT NULL DEFAULT 0 CHECK (depositor_amount >= 0),
    treasury_amount        NUMERIC(38,0) NOT NULL DEFAULT 0 CHECK (treasury_amount >= 0),
    retained_dust          NUMERIC(38,0) NOT NULL DEFAULT 0 CHECK (retained_dust >= 0),
    occurred_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_penalty_distributions_vault ON penalty_distributions (vault_contract_address, occurred_at DESC);
