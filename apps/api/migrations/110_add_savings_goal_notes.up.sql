-- Add notes column to savings_goals table (#929)
ALTER TABLE savings_goals ADD COLUMN IF NOT EXISTS notes TEXT;
