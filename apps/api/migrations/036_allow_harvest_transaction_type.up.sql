-- Migration 036: allow 'harvest' as a vault_transactions.type value.
--
-- Migration 008 created vault_transactions with CHECK (type IN ('deposit',
-- 'withdrawal')). Service-layer RecordHarvest writes 'harvest' rows for
-- completed yield claims, so any production harvest would fail the constraint.
-- This migration widens the allowed set to include 'harvest'. The constraint
-- is dropped by its auto-generated name (Postgres names
-- table_column_check when no name is supplied in CREATE TABLE).
ALTER TABLE vault_transactions
    DROP CONSTRAINT IF EXISTS vault_transactions_type_check;

ALTER TABLE vault_transactions
    ADD CONSTRAINT vault_transactions_type_check
    CHECK (type IN ('deposit', 'withdrawal', 'harvest'));
