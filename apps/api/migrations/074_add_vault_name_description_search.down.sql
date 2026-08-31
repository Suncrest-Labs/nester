DROP INDEX IF EXISTS idx_vaults_search_vector;
ALTER TABLE vaults DROP COLUMN IF EXISTS search_vector;
ALTER TABLE vaults DROP COLUMN IF EXISTS description;
ALTER TABLE vaults DROP COLUMN IF EXISTS name;
