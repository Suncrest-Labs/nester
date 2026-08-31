-- migration:irreversible
-- Revert vault balance columns to NUMERIC(20,8).
--
-- This narrowing is lossy by nature: any balance that needed the widened range
-- (>= 10^12) cannot be represented at NUMERIC(20,8) and PostgreSQL will raise
-- "numeric field overflow" rather than silently truncate. That failure is the
-- correct behaviour — a balance must never be rounded away by a schema change —
-- so the rollback deliberately does not coerce or clamp values. If this
-- migration must be rolled back on a database holding large balances, those
-- rows have to be reconciled first.

ALTER TABLE vaults
    ALTER COLUMN total_deposited TYPE NUMERIC(20, 8),
    ALTER COLUMN current_balance TYPE NUMERIC(20, 8),
    ALTER COLUMN yield_earned    TYPE NUMERIC(20, 8),
    ALTER COLUMN fees_paid       TYPE NUMERIC(20, 8);
