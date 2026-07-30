-- Constrain savings_goals.status to the known lifecycle values. Migration 040
-- added the column without a CHECK; this enforces the allowed set and makes
-- 'archived' a first-class status (#684).
ALTER TABLE savings_goals
  ADD CONSTRAINT savings_goals_status_check
  CHECK (status IN ('active', 'paused', 'completed', 'archived'));
