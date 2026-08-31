ALTER TABLE savings_goals
  DROP COLUMN IF EXISTS min_contribution,
  DROP COLUMN IF EXISTS max_contribution;
