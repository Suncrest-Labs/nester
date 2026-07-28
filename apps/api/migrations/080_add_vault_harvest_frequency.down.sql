ALTER TABLE vaults DROP CONSTRAINT IF EXISTS vaults_harvest_frequency_check;
ALTER TABLE vaults DROP COLUMN IF EXISTS last_harvested_at;
ALTER TABLE vaults DROP COLUMN IF EXISTS harvest_frequency;
