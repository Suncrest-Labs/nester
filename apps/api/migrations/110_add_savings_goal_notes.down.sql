-- Remove notes column from savings_goals table (#929)
ALTER TABLE savings_goals DROP COLUMN IF EXISTS notes;
