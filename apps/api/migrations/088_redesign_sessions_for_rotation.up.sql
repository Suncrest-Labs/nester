-- Redesign sessions for refresh-token rotation + revocation.
-- The 013 sessions table is unused by application code (no repository, no
-- service, no middleware check ever referenced it) and holds no production
-- data, so this is a clean redesign rather than an ALTER migration.

DROP TABLE IF EXISTS sessions;

CREATE TABLE sessions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    wallet_address      TEXT NOT NULL,
    device_fingerprint  TEXT NOT NULL,
    user_agent          TEXT,
    ip_address          TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_active_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    revoked_at          TIMESTAMPTZ,
    revoked_reason      TEXT
);

CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_wallet_address ON sessions(wallet_address);
CREATE INDEX idx_sessions_active ON sessions(user_id) WHERE revoked_at IS NULL;

CREATE TABLE refresh_tokens (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id     UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    token_hash     TEXT NOT NULL UNIQUE,
    parent_id      UUID REFERENCES refresh_tokens(id),
    issued_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at     TIMESTAMPTZ NOT NULL,
    used_at        TIMESTAMPTZ,
    revoked_at     TIMESTAMPTZ,
    revoked_reason TEXT
);

CREATE INDEX idx_refresh_tokens_session_id ON refresh_tokens(session_id);
