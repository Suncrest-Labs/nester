-- Periodic financial insight digests (issue #859).
-- digest_cadence controls how often a user receives a digest and is
-- delivered through the same channel preferences already on this row.
ALTER TABLE notification_preferences
    ADD COLUMN IF NOT EXISTS digest_cadence TEXT NOT NULL DEFAULT 'monthly'
        CHECK (digest_cadence IN ('off', 'weekly', 'monthly'));

-- user_digests is the authoritative cache/audit record for generated
-- digests: one row per user per period. facts_hash lets the generator
-- detect when the underlying period data changed (e.g. a corrected
-- transaction) and needs regeneration, so a normal re-check is a cheap
-- no-op instead of a fresh LLM call every time.
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
