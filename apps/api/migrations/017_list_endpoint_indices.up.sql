CREATE INDEX IF NOT EXISTS idx_vaults_user_created ON vaults(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_settlements_user_created ON settlements(user_id, created_at DESC);
