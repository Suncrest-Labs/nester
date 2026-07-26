CREATE TABLE IF NOT EXISTS market_context_signals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    protocol TEXT NOT NULL,
    asset TEXT,
    signal_type TEXT NOT NULL CHECK (
        signal_type IN ('announcement', 'security_concern', 'sentiment_shift', 'depeg_risk')
    ),
    direction TEXT NOT NULL CHECK (direction IN ('positive', 'negative', 'neutral')),
    confidence DOUBLE PRECISION NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    summary TEXT NOT NULL,
    source_url TEXT NOT NULL,
    publisher TEXT NOT NULL,
    corroborating_sources JSONB NOT NULL DEFAULT '[]'::jsonb,
    observed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (protocol, signal_type, direction, source_url)
);

CREATE INDEX idx_market_context_protocol_observed
    ON market_context_signals (protocol, observed_at DESC);
