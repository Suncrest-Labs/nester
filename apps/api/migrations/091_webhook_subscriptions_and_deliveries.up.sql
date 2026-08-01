-- Outbound webhook delivery system (nester#836): extends the existing
-- webhooks table into a full subscription (event-type filter, encrypted
-- secret, suspension state) and adds a per-attempt delivery log.

ALTER TABLE webhooks
    ADD COLUMN IF NOT EXISTS event_types TEXT[] NOT NULL DEFAULT '{}',
    -- The signing secret is encrypted at rest via AccountCipher
    -- (internal/crypto/account_cipher.go) rather than stored in plaintext.
    -- secret_key_version records which key version sealed it, so key
    -- rotation can still decrypt older subscriptions' secrets.
    ADD COLUMN IF NOT EXISTS secret_key_version TEXT,
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'suspended')),
    -- Counts consecutive dead-lettered deliveries (reset to 0 on any
    -- success). Crossing webhookDeadLetterSuspendThreshold auto-suspends the
    -- subscription and notifies the owner (#836's auto-suspend requirement).
    ADD COLUMN IF NOT EXISTS consecutive_dead_letters INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS suspended_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_webhooks_status ON webhooks (status);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id      UUID        NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    -- Included in every delivered payload so recipients can dedupe
    -- at-least-once redelivery (#836's documented integrator expectation).
    -- Stable across retries of the same logical delivery; a manual
    -- redelivery gets a fresh id (it is a new attempt chain, not a retry).
    delivery_id     UUID        NOT NULL,
    event_type      TEXT        NOT NULL,
    payload         BYTEA       NOT NULL,
    attempt         INT         NOT NULL,
    -- 'pending' | 'succeeded' | 'failed' | 'dead_letter'
    outcome         TEXT        NOT NULL,
    response_status INT,
    response_body_snippet TEXT,
    error           TEXT,
    duration_ms     INT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_webhook_id ON webhook_deliveries (webhook_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_delivery_id ON webhook_deliveries (delivery_id);
