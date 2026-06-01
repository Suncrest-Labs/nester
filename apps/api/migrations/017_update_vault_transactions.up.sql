ALTER TABLE vault_transactions ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE vault_transactions ADD COLUMN IF NOT EXISTS shares_minted_or_burned NUMERIC(28, 8);
ALTER TABLE vault_transactions ADD COLUMN IF NOT EXISTS share_price_at_time NUMERIC(28, 8);
ALTER TABLE vault_transactions ADD COLUMN IF NOT EXISTS fee_charged NUMERIC(28, 8);
ALTER TABLE vault_transactions RENAME COLUMN tx_hash TO transaction_hash;
