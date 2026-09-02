-- Best-effort recreation of the offramp tables as they stood when dropped
-- (006/014/016/017/034/057/111 combined). Data is not restored.
CREATE TABLE IF NOT EXISTS settlements (
    id                         UUID        PRIMARY KEY,
    user_id                    UUID        NOT NULL REFERENCES users(id)  ON DELETE RESTRICT,
    vault_id                   UUID        NOT NULL REFERENCES vaults(id) ON DELETE RESTRICT,
    amount                     NUMERIC(30, 8) NOT NULL CHECK (amount > 0),
    currency                   VARCHAR(10) NOT NULL,
    fiat_currency              VARCHAR(10) NOT NULL,
    fiat_amount                NUMERIC(30, 8) NOT NULL CHECK (fiat_amount > 0),
    exchange_rate              NUMERIC(30, 8) NOT NULL CHECK (exchange_rate > 0),
    destination_type           VARCHAR(50)  NOT NULL,
    destination_provider       VARCHAR(50)  NOT NULL,
    destination_account_number VARCHAR(100) NOT NULL,
    destination_account_name   VARCHAR(200) NOT NULL,
    destination_bank_code      VARCHAR(20)  NOT NULL DEFAULT '',
    status                     VARCHAR(30)  NOT NULL DEFAULT 'initiated'
                                    CHECK (status IN (
                                        'initiated',
                                        'liquidity_matched',
                                        'fiat_dispatched',
                                        'confirmed',
                                        'failed'
                                    )),
    retry_count                INTEGER NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
    error_message              TEXT,
    notes                      TEXT,
    estimated_fee              NUMERIC(30, 8),
    search_vector              TSVECTOR GENERATED ALWAYS AS (to_tsvector('english', COALESCE(notes, ''))) STORED,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at               TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_settlements_user_id ON settlements(user_id);
CREATE INDEX IF NOT EXISTS idx_settlements_vault_id ON settlements(vault_id);
CREATE INDEX IF NOT EXISTS idx_settlements_status ON settlements(status);
CREATE INDEX IF NOT EXISTS idx_settlements_status_created ON settlements(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_settlements_created ON settlements(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_settlements_user_created ON settlements(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_settlements_search_vector ON settlements USING GIN (search_vector);

CREATE TABLE IF NOT EXISTS bank_accounts (
    id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bank_name                  TEXT NOT NULL,
    bank_code                  TEXT,
    account_number_encrypted   BYTEA NOT NULL,
    account_number_fingerprint TEXT NOT NULL,
    account_last4              TEXT NOT NULL,
    account_name               TEXT NOT NULL,
    currency                   TEXT NOT NULL,
    country                    TEXT NOT NULL,
    is_default                 BOOLEAN NOT NULL DEFAULT false,
    key_version                VARCHAR(32) NOT NULL DEFAULT 'v1',
    verified_at                TIMESTAMPTZ,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_bank_accounts_user_fingerprint_bank
    ON bank_accounts (user_id, account_number_fingerprint, COALESCE(bank_code, ''));
CREATE UNIQUE INDEX IF NOT EXISTS idx_bank_accounts_one_default_per_currency
    ON bank_accounts (user_id, currency)
    WHERE is_default = true;
CREATE INDEX IF NOT EXISTS idx_bank_accounts_user_id ON bank_accounts(user_id);
CREATE INDEX IF NOT EXISTS idx_bank_accounts_key_version ON bank_accounts (key_version);
