ALTER TABLE settlements DROP CONSTRAINT IF EXISTS check_retry_count;
ALTER TABLE vaults DROP CONSTRAINT IF EXISTS check_current_balance;
ALTER TABLE allocations DROP CONSTRAINT IF EXISTS check_apy_range;

DROP INDEX IF EXISTS idx_users_wallet;
DROP INDEX IF EXISTS idx_allocations_vault_allocated;
DROP INDEX IF EXISTS idx_settlements_created;
DROP INDEX IF EXISTS idx_settlements_status_created;
DROP INDEX IF EXISTS idx_vaults_user_status;
DROP INDEX IF EXISTS idx_vaults_currency;
DROP INDEX IF EXISTS idx_vaults_status;
