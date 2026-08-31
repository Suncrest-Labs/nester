ALTER TABLE savings_goals
    DROP COLUMN IF EXISTS yield_balance,
    DROP COLUMN IF EXISTS auto_compound;
