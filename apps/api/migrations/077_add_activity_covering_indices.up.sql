-- Backs the GET /api/v1/activity union feed: vault_transactions previously
-- had only a vault_id index, forcing a full scan for any user-scoped,
-- time-ordered read (the shape every branch of that query needs).
CREATE INDEX IF NOT EXISTS idx_vault_transactions_user_created ON vault_transactions(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_vault_transactions_user_type_created ON vault_transactions(user_id, type, created_at DESC);
