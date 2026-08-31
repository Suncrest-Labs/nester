DROP TABLE IF EXISTS ledger_checkpoints;

ALTER TABLE processed_events
DROP COLUMN IF EXISTS ledger_sequence,
DROP COLUMN IF EXISTS tx_hash,
DROP COLUMN IF EXISTS event_index;
