-- Revert: drop new indexes and restore original CHECK constraint
DROP INDEX IF EXISTS idx_transactions_created_at_id;
DROP INDEX IF EXISTS idx_transactions_status;

ALTER TABLE transactions
    DROP CONSTRAINT IF EXISTS transactions_type_check;

ALTER TABLE transactions
    ADD CONSTRAINT transactions_type_check
    CHECK (type IN ('deposit', 'withdrawal', 'allocation', 'settlement'));
