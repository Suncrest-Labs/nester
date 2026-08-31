-- Backfill/rebuild range-scoping (nester#840): penalty_events,
-- penalty_distributions, vault_rebalance_legs and vault_rebalance_completions
-- previously carried only occurred_at (wall-clock insert time), which cannot
-- be mapped back to the ledger range an event came from — a backfill run
-- long after the fact would insert rows with a recent occurred_at for old
-- ledgers, making "clear rows for this ledger range" impossible to express
-- correctly. ledger_sequence is populated by the indexer's apply* handlers
-- going forward (see internal/stellar/indexer.go) and is what
-- Runner.resetScope now filters DELETEs on, instead of clearing every row
-- for a contract regardless of range.
ALTER TABLE penalty_events ADD COLUMN IF NOT EXISTS ledger_sequence BIGINT;
ALTER TABLE penalty_distributions ADD COLUMN IF NOT EXISTS ledger_sequence BIGINT;
ALTER TABLE vault_rebalance_legs ADD COLUMN IF NOT EXISTS ledger_sequence BIGINT;
ALTER TABLE vault_rebalance_completions ADD COLUMN IF NOT EXISTS ledger_sequence BIGINT;

CREATE INDEX IF NOT EXISTS idx_penalty_events_ledger_sequence ON penalty_events (ledger_sequence);
CREATE INDEX IF NOT EXISTS idx_penalty_distributions_ledger_sequence ON penalty_distributions (ledger_sequence);
CREATE INDEX IF NOT EXISTS idx_vault_rebalance_legs_ledger_sequence ON vault_rebalance_legs (ledger_sequence);
CREATE INDEX IF NOT EXISTS idx_vault_rebalance_completions_ledger_sequence ON vault_rebalance_completions (ledger_sequence);
