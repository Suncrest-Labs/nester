-- Restore the reorg-checkpoint schema exactly as 100_reorg_safe_indexer left
-- it, so rolling back to a release that still shipped ReorgSafeIndexer finds
-- the objects it expects. Both were empty when 105 dropped them, so there is
-- no data to restore.

CREATE TABLE IF NOT EXISTS ledger_checkpoints (
  ledger_sequence BIGINT PRIMARY KEY,
  ledger_hash TEXT NOT NULL,
  parent_hash TEXT,
  processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  is_finalized BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_ledger_checkpoints_sequence ON ledger_checkpoints(ledger_sequence DESC);
CREATE INDEX IF NOT EXISTS idx_ledger_checkpoints_finalized ON ledger_checkpoints(is_finalized) WHERE NOT is_finalized;

CREATE UNIQUE INDEX IF NOT EXISTS idx_processed_events_dedup
ON processed_events(ledger_sequence, tx_hash, event_index)
WHERE ledger_sequence IS NOT NULL AND tx_hash IS NOT NULL;
