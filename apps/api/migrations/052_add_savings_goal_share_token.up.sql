ALTER TABLE savings_goals
    ADD COLUMN IF NOT EXISTS share_token      UUID        DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS share_enabled_at TIMESTAMPTZ DEFAULT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_savings_goals_share_token
    ON savings_goals (share_token) WHERE share_token IS NOT NULL;
