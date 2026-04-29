CREATE INDEX IF NOT EXISTS idx_vaults_status ON vaults(status);
CREATE INDEX IF NOT EXISTS idx_vaults_currency ON vaults(currency);
CREATE INDEX IF NOT EXISTS idx_vaults_user_status ON vaults(user_id, status);
CREATE INDEX IF NOT EXISTS idx_settlements_status_created ON settlements(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_settlements_created ON settlements(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_allocations_vault_allocated ON allocations(vault_id, allocated_at DESC);
CREATE INDEX IF NOT EXISTS idx_users_wallet ON users(wallet_address);

ALTER TABLE allocations ADD CONSTRAINT check_apy_range CHECK (apy >= 0 AND apy <= 100);
ALTER TABLE vaults ADD CONSTRAINT check_current_balance CHECK (current_balance >= 0);
ALTER TABLE settlements ADD CONSTRAINT check_retry_count CHECK (retry_count >= 0);
