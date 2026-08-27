-- Revert the money-path invariants (nester#1083).
--
-- Dropping these does not lose data; it only stops the database rejecting
-- rows that violate them. Any row written while they were absent stays.

ALTER TABLE vaults DROP CONSTRAINT IF EXISTS check_vaults_fees_paid_non_negative;
ALTER TABLE vaults DROP CONSTRAINT IF EXISTS check_vaults_yield_earned_non_negative;

ALTER TABLE vault_transactions DROP CONSTRAINT IF EXISTS check_vault_tx_share_price_positive;
ALTER TABLE vault_transactions DROP CONSTRAINT IF EXISTS check_vault_tx_shares_non_negative;

DROP INDEX IF EXISTS uq_vault_transactions_tx_hash;
DROP INDEX IF EXISTS uq_vaults_contract_address_live;
