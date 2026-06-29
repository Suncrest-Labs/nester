CREATE TABLE IF NOT EXISTS savings_goal_deposits (
    id         UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    goal_id    UUID          NOT NULL REFERENCES savings_goals(id) ON DELETE CASCADE,
    user_id    UUID          NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount     NUMERIC(20,8) NOT NULL CHECK (amount > 0),
    currency   VARCHAR(10)   NOT NULL,
    created_at TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_savings_goal_deposits_goal_id ON savings_goal_deposits(goal_id);
CREATE INDEX IF NOT EXISTS idx_savings_goal_deposits_user_id ON savings_goal_deposits(user_id);
