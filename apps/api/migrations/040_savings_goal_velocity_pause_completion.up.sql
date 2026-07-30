ALTER TABLE savings_goals ADD COLUMN IF NOT EXISTS status VARCHAR(20) NOT NULL DEFAULT 'active';
ALTER TABLE savings_goals ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;
ALTER TABLE savings_goals ADD COLUMN IF NOT EXISTS completion_action VARCHAR(20);

CREATE INDEX IF NOT EXISTS idx_savings_goals_user_status ON savings_goals(user_id, status);
