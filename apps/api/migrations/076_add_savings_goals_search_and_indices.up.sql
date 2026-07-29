ALTER TABLE savings_goals ADD COLUMN IF NOT EXISTS search_vector tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('english', coalesce(name, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(description, '')), 'B')
    ) STORED;

CREATE INDEX IF NOT EXISTS idx_savings_goals_search_vector ON savings_goals USING GIN (search_vector);
CREATE INDEX IF NOT EXISTS idx_savings_goals_user_category ON savings_goals(user_id, category);
CREATE INDEX IF NOT EXISTS idx_savings_goals_user_created ON savings_goals(user_id, created_at DESC);
