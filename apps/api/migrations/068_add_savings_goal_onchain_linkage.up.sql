-- Links a savings goal to its on-chain twin in the savings_goal registry
-- contract (#807). onchain_goal_id is the hex-encoded BytesN<32> derived
-- from hashing this goal's UUID off-chain; onchain_status mirrors the
-- contract's GoalStatus enum (active/completed/abandoned/expired) and is
-- kept separate from the existing `status` column since on-chain
-- registration happens asynchronously and can lag or fail independently of
-- the backend's own lifecycle state.
ALTER TABLE savings_goals
    ADD COLUMN IF NOT EXISTS onchain_goal_id TEXT NULL,
    ADD COLUMN IF NOT EXISTS onchain_status TEXT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_savings_goals_onchain_goal_id
    ON savings_goals (onchain_goal_id)
    WHERE onchain_goal_id IS NOT NULL;
