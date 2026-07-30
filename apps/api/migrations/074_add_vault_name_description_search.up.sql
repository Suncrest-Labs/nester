ALTER TABLE vaults ADD COLUMN IF NOT EXISTS name TEXT;
ALTER TABLE vaults ADD COLUMN IF NOT EXISTS description TEXT;

ALTER TABLE vaults ADD COLUMN IF NOT EXISTS search_vector tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('english', coalesce(name, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(description, '')), 'B')
    ) STORED;

CREATE INDEX IF NOT EXISTS idx_vaults_search_vector ON vaults USING GIN (search_vector);
