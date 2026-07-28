-- DELETE /savings-goals/{id} now soft-archives instead of destroying the row
-- (#685). archived_at records when the goal was archived via deletion; goals
-- archived through the status endpoint before this migration keep a NULL
-- archived_at.
ALTER TABLE savings_goals
    ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ NULL;
