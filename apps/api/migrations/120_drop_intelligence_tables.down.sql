-- Best-effort recreation of the AI-feature tables as they stood when
-- dropped (066/079). Data is not restored.
ALTER TABLE notification_preferences
    ADD COLUMN IF NOT EXISTS digest_cadence TEXT NOT NULL DEFAULT 'monthly'
        CHECK (digest_cadence IN ('off', 'weekly', 'monthly'));

CREATE TABLE IF NOT EXISTS user_digests (
    id              UUID PRIMARY KEY,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    period          TEXT NOT NULL CHECK (period IN ('weekly', 'monthly')),
    period_start    DATE NOT NULL,
    period_end      DATE NOT NULL,
    facts_hash      TEXT NOT NULL,
    facts           JSONB NOT NULL,
    narrative       TEXT NOT NULL,
    attention_items JSONB NOT NULL DEFAULT '[]'::jsonb,
    honest_zero_period BOOLEAN NOT NULL DEFAULT FALSE,
    delivered_at    TIMESTAMPTZ,
    generated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, period, period_start)
);
CREATE INDEX IF NOT EXISTS idx_user_digests_user_period
    ON user_digests (user_id, period, period_start DESC);

CREATE TABLE IF NOT EXISTS tool_invocations (
    id UUID PRIMARY KEY,
    user_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    conversation_id TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    arguments JSONB NOT NULL,
    consequential BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT NOT NULL,
    result JSONB,
    error_message TEXT,
    prev_hash TEXT NOT NULL,
    entry_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_tool_invocations_user_id_created_at
    ON tool_invocations (user_id, created_at DESC);
