-- Stores point-in-time TVL snapshots for DeFiLlama protocols so the health
-- checker can compute 24-hour drops without making extra upstream calls.
CREATE TABLE IF NOT EXISTS protocol_tvl_snapshots (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    protocol_slug  TEXT        NOT NULL,
    tvl_usd        NUMERIC(28, 2) NOT NULL,
    snapshotted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_protocol_tvl_snapshots_slug_time
    ON protocol_tvl_snapshots (protocol_slug, snapshotted_at DESC);

-- Tracks the last time a ProtocolHealthAlert was sent per protocol so we
-- can enforce the 12-hour re-alert cooldown.
CREATE TABLE IF NOT EXISTS protocol_health_alerts (
    protocol_slug TEXT        PRIMARY KEY,
    last_alerted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
