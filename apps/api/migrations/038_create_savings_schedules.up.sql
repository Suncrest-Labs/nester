CREATE TABLE IF NOT EXISTS savings_schedules (
    id            UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID          NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    goal_id       UUID          NOT NULL REFERENCES savings_goals(id) ON DELETE CASCADE,
    vault_id      UUID          NOT NULL REFERENCES vaults(id),
    amount        NUMERIC(20,8) NOT NULL CHECK (amount > 0),
    currency      VARCHAR(10)   NOT NULL DEFAULT 'USDC',
    frequency     VARCHAR(20)   NOT NULL CHECK (frequency IN ('weekly', 'monthly')),
    next_run_at   TIMESTAMPTZ   NOT NULL,
    last_run_at   TIMESTAMPTZ   NULL,
    is_active     BOOLEAN       NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_savings_schedules_goal_id ON savings_schedules(goal_id);
CREATE INDEX IF NOT EXISTS idx_savings_schedules_user_id ON savings_schedules(user_id);
CREATE INDEX IF NOT EXISTS idx_savings_schedules_due ON savings_schedules(next_run_at) WHERE is_active = TRUE;

CREATE UNIQUE INDEX IF NOT EXISTS idx_savings_schedules_one_active_per_goal
    ON savings_schedules(goal_id) WHERE is_active = TRUE;
