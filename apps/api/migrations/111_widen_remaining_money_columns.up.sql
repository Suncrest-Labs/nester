-- Widen the remaining money columns to the full i128 stroop range (issue #1121).
--
-- Migration 103 widened the four vault balance columns after #1074, but that fix
-- was local to the vaults table. The audit in #1121 found the same overflow
-- waiting on every other column carrying an amount: NUMERIC(20,8) allows only 12
-- integer digits and NUMERIC(28,8) only 20, while Soroban token amounts are i128
-- stroops reaching ~1.7e38.
--
-- A deposit large enough to be rejected by `transactions.amount` is an ordinary
-- vault deposit, not an edge case, and the failure mode is a hard
-- "numeric field overflow" at write time.
--
-- NUMERIC(48,8) holds 40 integer digits, covering the full i128 range with
-- headroom, and matches the precedent set by migration 103. Widening precision
-- is a metadata-only change in PostgreSQL and preserves every stored value
-- exactly.
--
-- Scale note: savings_goals.target_amount is widened from NUMERIC(20,7) to
-- NUMERIC(48,8). Increasing the scale is also lossless — stored values are
-- re-expressed with one additional decimal place.

ALTER TABLE transactions
    ALTER COLUMN amount TYPE NUMERIC(48, 8);

ALTER TABLE settlements
    ALTER COLUMN amount        TYPE NUMERIC(48, 8),
    ALTER COLUMN fiat_amount   TYPE NUMERIC(48, 8),
    ALTER COLUMN estimated_fee TYPE NUMERIC(48, 8);

ALTER TABLE vault_transactions
    ALTER COLUMN amount      TYPE NUMERIC(48, 8),
    ALTER COLUMN fee_charged TYPE NUMERIC(48, 8);

ALTER TABLE vault_performance_snapshots
    ALTER COLUMN total_balance      TYPE NUMERIC(48, 8),
    ALTER COLUMN total_deposited    TYPE NUMERIC(48, 8),
    ALTER COLUMN total_yield_earned TYPE NUMERIC(48, 8);

ALTER TABLE savings_goals
    ALTER COLUMN target_amount TYPE NUMERIC(48, 8),
    ALTER COLUMN yield_balance TYPE NUMERIC(48, 8);

ALTER TABLE savings_schedules
    ALTER COLUMN amount TYPE NUMERIC(48, 8);

ALTER TABLE savings_goal_deposits
    ALTER COLUMN amount TYPE NUMERIC(48, 8);

ALTER TABLE yield_harvests
    ALTER COLUMN amount TYPE NUMERIC(48, 8);

ALTER TABLE goal_templates
    ALTER COLUMN suggested_amount TYPE NUMERIC(48, 8);

ALTER TABLE allocations
    ALTER COLUMN amount TYPE NUMERIC(48, 8);

-- These tables store raw stroops at scale 0. NUMERIC(38,0) is one integer digit
-- short of the i128 maximum (~1.7e38 needs 39 digits), so the largest
-- representable amounts overflow. Widened to NUMERIC(48,0) to match the headroom
-- the scaled columns above get.

ALTER TABLE penalty_events
    ALTER COLUMN amount        TYPE NUMERIC(48, 0),
    ALTER COLUMN shares_burned TYPE NUMERIC(48, 0);

ALTER TABLE penalty_distributions
    ALTER COLUMN depositor_amount TYPE NUMERIC(48, 0),
    ALTER COLUMN treasury_amount  TYPE NUMERIC(48, 0),
    ALTER COLUMN retained_dust    TYPE NUMERIC(48, 0);

ALTER TABLE vault_rebalance_legs
    ALTER COLUMN delta      TYPE NUMERIC(48, 0),
    ALTER COLUMN amount_out TYPE NUMERIC(48, 0),
    ALTER COLUMN min_out    TYPE NUMERIC(48, 0);

ALTER TABLE emergency_withdrawal_queue
    ALTER COLUMN shares_requested TYPE NUMERIC(48, 0),
    ALTER COLUMN shares_filled    TYPE NUMERIC(48, 0);
