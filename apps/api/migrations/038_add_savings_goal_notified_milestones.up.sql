ALTER TABLE savings_goals
    ADD COLUMN IF NOT EXISTS notified_milestones INTEGER[] NOT NULL DEFAULT '{}';
