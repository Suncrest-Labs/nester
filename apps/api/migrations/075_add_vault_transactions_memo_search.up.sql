-- vault_transactions: memo column + full-text search, and a CHECK-constraint
-- fix. Migration 036 widened the type CHECK to ('deposit', 'withdrawal',
-- 'harvest') but VaultRepository.RecordRebalance inserts a literal
-- type = 'rebalance' row — every user-triggered rebalance has been failing
-- and rolling back its whole transaction in production. Widen the
-- constraint to include it, same pattern as migration 036.
ALTER TABLE vault_transactions DROP CONSTRAINT IF EXISTS vault_transactions_type_check;
ALTER TABLE vault_transactions ADD CONSTRAINT vault_transactions_type_check
    CHECK (type IN ('deposit', 'withdrawal', 'harvest', 'rebalance'));

ALTER TABLE vault_transactions ADD COLUMN IF NOT EXISTS memo TEXT;

ALTER TABLE vault_transactions ADD COLUMN IF NOT EXISTS search_vector tsvector
    GENERATED ALWAYS AS (to_tsvector('english', coalesce(memo, ''))) STORED;

CREATE INDEX IF NOT EXISTS idx_vault_transactions_search_vector ON vault_transactions USING GIN (search_vector);

-- settlements: full-text search over the existing notes column.
ALTER TABLE settlements ADD COLUMN IF NOT EXISTS search_vector tsvector
    GENERATED ALWAYS AS (to_tsvector('english', coalesce(notes, ''))) STORED;

CREATE INDEX IF NOT EXISTS idx_settlements_search_vector ON settlements USING GIN (search_vector);
