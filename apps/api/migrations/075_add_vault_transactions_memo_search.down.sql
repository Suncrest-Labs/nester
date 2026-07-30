DROP INDEX IF EXISTS idx_settlements_search_vector;
ALTER TABLE settlements DROP COLUMN IF EXISTS search_vector;

DROP INDEX IF EXISTS idx_vault_transactions_search_vector;
ALTER TABLE vault_transactions DROP COLUMN IF EXISTS search_vector;
ALTER TABLE vault_transactions DROP COLUMN IF EXISTS memo;

ALTER TABLE vault_transactions DROP CONSTRAINT IF EXISTS vault_transactions_type_check;
ALTER TABLE vault_transactions ADD CONSTRAINT vault_transactions_type_check
    CHECK (type IN ('deposit', 'withdrawal', 'harvest'));
