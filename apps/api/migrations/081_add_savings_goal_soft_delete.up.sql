-- #924: real soft-delete for savings goals with a 30-day recovery window.
-- deleted_at is distinct from the existing archived_at/status='archived'
-- pair (#685, #684): archiving is a user-visible lifecycle state the user
-- can toggle back and forth, while deleted_at marks a goal the user deleted
-- and hides it from all normal reads until either restored or permanently
-- purged by the recovery-window scheduler.
ALTER TABLE savings_goals
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS idx_savings_goals_deleted_at ON savings_goals(deleted_at) WHERE deleted_at IS NOT NULL;
