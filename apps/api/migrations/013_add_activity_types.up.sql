-- Extend the type CHECK constraint to include rebalance and yield_earned
ALTER TABLE transactions
    DROP CONSTRAINT IF EXISTS transactions_type_check;

ALTER TABLE transactions
    ADD CONSTRAINT transactions_type_check
    CHECK (type IN ('deposit', 'withdrawal', 'allocation', 'settlement', 'rebalance', 'yield_earned'));

-- Index for efficient cursor-based pagination (created_at DESC, id DESC)
CREATE INDEX IF NOT EXISTS idx_transactions_created_at_id
    ON transactions (created_at DESC, id DESC);

-- Index for filtering by status
CREATE INDEX IF NOT EXISTS idx_transactions_status
    ON transactions (status);
