-- Migration 036 (down): restore the pre-harvest CHECK constraint on
-- vault_transactions.type. Will fail if any 'harvest' rows still exist —
-- that is the desired fail-loud behaviour so operators notice stale data
-- before locking writes out.
ALTER TABLE vault_transactions
    DROP CONSTRAINT IF EXISTS vault_transactions_type_check;

ALTER TABLE vault_transactions
    ADD CONSTRAINT vault_transactions_type_check
    CHECK (type IN ('deposit', 'withdrawal'));
