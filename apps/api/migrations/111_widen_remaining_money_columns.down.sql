-- Revert the money columns widened by 111 to their previous precision.
--
-- This narrowing is lossy by nature: any amount that needed the widened range
-- cannot be represented at the original precision and PostgreSQL will raise
-- "numeric field overflow" rather than silently truncate. That failure is the
-- correct behaviour — an amount must never be rounded away by a schema change —
-- so this rollback deliberately does not coerce or clamp values. If it must be
-- run against a database holding large amounts, those rows have to be
-- reconciled first.
--
-- Rolling this back is therefore unsafe once real i128-scale amounts have been
-- written. See docs/DEPLOYMENT.md for the irreversible-migration policy.

ALTER TABLE emergency_withdrawal_queue
    ALTER COLUMN shares_requested TYPE NUMERIC(38, 0),
    ALTER COLUMN shares_filled    TYPE NUMERIC(38, 0);

ALTER TABLE vault_rebalance_legs
    ALTER COLUMN delta      TYPE NUMERIC(38, 0),
    ALTER COLUMN amount_out TYPE NUMERIC(38, 0),
    ALTER COLUMN min_out    TYPE NUMERIC(38, 0);

ALTER TABLE penalty_distributions
    ALTER COLUMN depositor_amount TYPE NUMERIC(38, 0),
    ALTER COLUMN treasury_amount  TYPE NUMERIC(38, 0),
    ALTER COLUMN retained_dust    TYPE NUMERIC(38, 0);

ALTER TABLE penalty_events
    ALTER COLUMN amount        TYPE NUMERIC(38, 0),
    ALTER COLUMN shares_burned TYPE NUMERIC(38, 0);

ALTER TABLE allocations
    ALTER COLUMN amount TYPE NUMERIC(20, 8);

ALTER TABLE goal_templates
    ALTER COLUMN suggested_amount TYPE NUMERIC(20, 8);

ALTER TABLE yield_harvests
    ALTER COLUMN amount TYPE NUMERIC(28, 8);

ALTER TABLE savings_goal_deposits
    ALTER COLUMN amount TYPE NUMERIC(20, 8);

ALTER TABLE savings_schedules
    ALTER COLUMN amount TYPE NUMERIC(20, 8);

ALTER TABLE savings_goals
    ALTER COLUMN target_amount TYPE NUMERIC(20, 7),
    ALTER COLUMN yield_balance TYPE NUMERIC(20, 8);

ALTER TABLE vault_performance_snapshots
    ALTER COLUMN total_balance      TYPE NUMERIC(20, 8),
    ALTER COLUMN total_deposited    TYPE NUMERIC(20, 8),
    ALTER COLUMN total_yield_earned TYPE NUMERIC(20, 8);

ALTER TABLE vault_transactions
    ALTER COLUMN amount      TYPE NUMERIC(28, 8),
    ALTER COLUMN fee_charged TYPE NUMERIC(28, 8);

ALTER TABLE settlements
    ALTER COLUMN amount        TYPE NUMERIC(20, 8),
    ALTER COLUMN fiat_amount   TYPE NUMERIC(20, 8),
    ALTER COLUMN estimated_fee TYPE NUMERIC(20, 8);

ALTER TABLE transactions
    ALTER COLUMN amount TYPE NUMERIC(20, 8);
