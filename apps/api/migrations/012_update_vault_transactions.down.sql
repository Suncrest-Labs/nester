ALTER TABLE vault_transactions RENAME COLUMN transaction_hash TO tx_hash;
ALTER TABLE vault_transactions DROP COLUMN IF EXISTS fee_charged;
ALTER TABLE vault_transactions DROP COLUMN IF EXISTS share_price_at_time;
ALTER TABLE vault_transactions DROP COLUMN IF EXISTS shares_minted_or_burned;
ALTER TABLE vault_transactions DROP COLUMN IF EXISTS user_id;
