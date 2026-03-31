ALTER TABLE vaults ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_vaults_deleted_at ON vaults (deleted_at) WHERE deleted_at IS NULL;
