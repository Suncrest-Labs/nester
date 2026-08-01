ALTER TABLE penalty_events DROP COLUMN IF EXISTS ledger_sequence;
ALTER TABLE penalty_distributions DROP COLUMN IF EXISTS ledger_sequence;
ALTER TABLE vault_rebalance_legs DROP COLUMN IF EXISTS ledger_sequence;
ALTER TABLE vault_rebalance_completions DROP COLUMN IF EXISTS ledger_sequence;
