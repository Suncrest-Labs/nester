ALTER TABLE savings_goals
    ADD COLUMN IF NOT EXISTS deadline_reminders_sent INTEGER[] NOT NULL DEFAULT '{}';
