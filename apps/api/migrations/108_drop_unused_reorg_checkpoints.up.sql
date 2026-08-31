-- Drop the reorg-checkpoint schema introduced by 100_reorg_safe_indexer (#1089).
--
-- ledger_checkpoints and idx_processed_events_dedup existed only to serve
-- ReorgSafeIndexer, which never had a non-test caller: EventPoller — the code
-- that actually indexes production — never wrote a checkpoint and never set
-- processed_events.tx_hash. Both objects have therefore always been empty, so
-- dropping them removes no data and changes no behaviour.
--
-- Reorg handling is documented as out of scope in
-- docs/event-indexer-replay.md ("Not covered: ledger reorganisations"), which
-- also records what re-introducing it would require. Reinstating this schema
-- is the last step of that work, not the first: an empty checkpoint table
-- reads as "reorgs are handled here" to anyone auditing the indexer.
--
-- processed_events.tx_hash and processed_events.event_index are deliberately
-- left in place. They are unwritten, but dropping columns from a money-path
-- table earns nothing here, and 100's own down migration already mishandles
-- them (it drops ledger_sequence, which migration 028 owns and the indexer
-- actively uses). Removing the index means nothing depends on them.

DROP INDEX IF EXISTS idx_processed_events_dedup;

DROP TABLE IF EXISTS ledger_checkpoints;
