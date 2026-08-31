-- Indexes for high-frequency money-path queries (nester#1138).
--
-- Balance and history queries run on every dashboard load and transaction list view.
-- Composite indexes eliminate sequential scans across growing transaction volumes.

-- 1. Vault transactions by vault ordered by time (hot path for vault activity feed)
CREATE INDEX IF NOT EXISTS idx_vault_transactions_vault_id_created_at
    ON vault_transactions (vault_id, created_at DESC);

-- 2. Vaults by user and status (hot path for active balance summaries)
CREATE INDEX IF NOT EXISTS idx_vaults_user_id_status_live
    ON vaults (user_id, status)
    WHERE deleted_at IS NULL;

-- 3. Yield harvests by vault ordered by time (hot path for performance calculation)
CREATE INDEX IF NOT EXISTS idx_yield_harvests_vault_id_harvested_at
    ON yield_harvests (vault_id, harvested_at DESC);
