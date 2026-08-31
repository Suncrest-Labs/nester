ALTER TABLE users ADD COLUMN IF NOT EXISTS timezone TEXT NOT NULL DEFAULT 'UTC';

CREATE TABLE IF NOT EXISTS savings_gamification_state (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    current_streak_days INTEGER NOT NULL DEFAULT 0,
    longest_streak_days INTEGER NOT NULL DEFAULT 0,
    last_qualified_day TEXT NOT NULL DEFAULT '',
    grace_used_for_day TEXT NOT NULL DEFAULT '',
    total_saved NUMERIC(46, 7) NOT NULL DEFAULT 0,
    goals_completed INTEGER NOT NULL DEFAULT 0,
    current_level INTEGER NOT NULL DEFAULT 1,
    durable_score NUMERIC(30, 7) NOT NULL DEFAULT 0,
    awarded_achievements TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS savings_gamification_events (
    event_id TEXT PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    ledger_sequence BIGINT,
    amount NUMERIC(46, 7) NOT NULL DEFAULT 0,
    net_amount NUMERIC(46, 7) NOT NULL DEFAULT 0,
    occurred_at TIMESTAMPTZ NOT NULL,
    qualified BOOLEAN NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    transition JSONB NOT NULL DEFAULT '{}',
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS savings_achievements (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code TEXT NOT NULL,
    awarded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, code)
);

CREATE INDEX IF NOT EXISTS idx_savings_gamification_events_user_time
ON savings_gamification_events(user_id, occurred_at DESC);
