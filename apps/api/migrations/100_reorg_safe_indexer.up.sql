CREATE TABLE ledger_checkpoints (
  ledger_sequence BIGINT PRIMARY KEY,
  ledger_hash TEXT NOT NULL,
  parent_hash TEXT,
  processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  is_finalized BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX idx_ledger_checkpoints_sequence ON ledger_checkpoints(ledger_sequence DESC);
CREATE INDEX idx_ledger_checkpoints_finalized ON ledger_checkpoints(is_finalized) WHERE NOT is_finalized;

ALTER TABLE processed_events
ADD COLUMN IF NOT EXISTS ledger_sequence BIGINT,
ADD COLUMN IF NOT EXISTS tx_hash TEXT,
ADD COLUMN IF NOT EXISTS event_index INTEGER;

CREATE UNIQUE INDEX IF NOT EXISTS idx_processed_events_dedup 
ON processed_events(ledger_sequence, tx_hash, event_index)
WHERE ledger_sequence IS NOT NULL AND tx_hash IS NOT NULL;
