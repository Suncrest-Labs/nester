CREATE TABLE IF NOT EXISTS savings_streaks (
    user_id              UUID        PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    current_streak       INTEGER     NOT NULL DEFAULT 0,
    longest_streak       INTEGER     NOT NULL DEFAULT 0,
    last_deposit_week    TEXT        NOT NULL DEFAULT '',
    notified_milestones  INTEGER[]   NOT NULL DEFAULT '{}',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
