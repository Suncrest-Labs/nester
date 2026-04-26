ALTER TABLE transactions
    DROP COLUMN IF EXISTS error_reason,
    DROP COLUMN IF EXISTS confirmed_at;
