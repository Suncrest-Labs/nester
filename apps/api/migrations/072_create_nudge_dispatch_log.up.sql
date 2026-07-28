CREATE TABLE nudge_dispatch_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    nudge_type TEXT NOT NULL,
    dedup_key TEXT NOT NULL,
    channel TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    copy_source TEXT NOT NULL CHECK (copy_source IN ('template', 'llm')),
    segment TEXT NOT NULL,
    sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, dedup_key)
);

CREATE TABLE nudge_outcomes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dispatch_id UUID NOT NULL REFERENCES nudge_dispatch_log(id) ON DELETE CASCADE,
    outcome_type TEXT NOT NULL CHECK (outcome_type IN ('deposit', 'goal_completed', 'return_visit')),
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    hours_after_dispatch NUMERIC NOT NULL
);

CREATE INDEX idx_nudge_dispatch_log_user_time ON nudge_dispatch_log(user_id, sent_at DESC);
CREATE INDEX idx_nudge_outcomes_dispatch ON nudge_outcomes(dispatch_id);
