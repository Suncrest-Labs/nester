CREATE TABLE IF NOT EXISTS apy_snapshots (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    protocol_slug TEXT        NOT NULL,
    apy           NUMERIC(10, 6) NOT NULL,
    tvl           NUMERIC(28, 8) NOT NULL DEFAULT 0,
    captured_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_apy_snapshots_protocol_slug ON apy_snapshots (protocol_slug);
CREATE INDEX IF NOT EXISTS idx_apy_snapshots_captured_at ON apy_snapshots (captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_apy_snapshots_protocol_time ON apy_snapshots (protocol_slug, captured_at DESC);
