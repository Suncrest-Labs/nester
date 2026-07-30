CREATE TABLE IF NOT EXISTS savings_goal_notification_preferences (
    goal_id UUID PRIMARY KEY REFERENCES savings_goals(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    muted BOOLEAN NOT NULL DEFAULT false,
    digest_frequency TEXT NOT NULL DEFAULT 'immediate',
    last_digest_sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_goal_notification_prefs_digest_frequency
        CHECK (digest_frequency IN ('immediate', 'daily', 'weekly'))
);

CREATE INDEX IF NOT EXISTS idx_goal_notification_prefs_user_id ON savings_goal_notification_preferences(user_id);

CREATE TABLE IF NOT EXISTS goal_notification_digest_queue (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    goal_id UUID NOT NULL REFERENCES savings_goals(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_goal_notification_digest_queue_goal_id ON goal_notification_digest_queue(goal_id);
