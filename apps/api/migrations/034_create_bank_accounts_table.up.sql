CREATE TABLE bank_accounts (
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
    verified_at                TIMESTAMPTZ,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, account_number_fingerprint, COALESCE(bank_code, ''))
);

CREATE UNIQUE INDEX idx_bank_accounts_one_default_per_currency
    ON bank_accounts (user_id, currency)
    WHERE is_default = true;

CREATE INDEX idx_bank_accounts_user_id ON bank_accounts(user_id);
