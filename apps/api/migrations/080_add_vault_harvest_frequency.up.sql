ALTER TABLE vaults
    ADD COLUMN harvest_frequency TEXT NOT NULL DEFAULT 'daily',
    ADD COLUMN last_harvested_at TIMESTAMPTZ;

ALTER TABLE vaults
    ADD CONSTRAINT vaults_harvest_frequency_check
    CHECK (harvest_frequency IN ('daily', 'weekly'));
