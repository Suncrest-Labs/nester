CREATE TABLE IF NOT EXISTS processed_events (
    event_id         TEXT PRIMARY KEY,
    ledger_sequence  BIGINT NOT NULL,
    processed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_processed_events_ledger_sequence ON processed_events (ledger_sequence);
