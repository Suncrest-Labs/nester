-- Money-path invariants enforced by the database (nester#1083).
--
-- Correctness has so far depended entirely on application code. A bug, a
-- migration, or a manual query can write a negative balance or attribute an
-- on-chain transaction to two vaults. These constraints make the database
-- refuse rather than trusting every future caller to get it right.
--
-- Each statement is guarded so the migration is safe to re-run and safe on a
-- database that already satisfies the invariant.

-- 1. One vault per contract address.
--
-- The event indexer keys balance mutations on contract_address
-- (`UPDATE vaults ... WHERE contract_address = $1`). Without uniqueness the
-- UPDATE matches every row sharing that address, so registering a vault
-- pointing at someone else's contract credits their on-chain deposits to the
-- attacker's vault as well. It also makes chain-event attribution ambiguous.
--
-- Scoped to live rows: a soft-deleted vault must not block re-registration.
CREATE UNIQUE INDEX IF NOT EXISTS uq_vaults_contract_address_live
    ON vaults (contract_address)
    WHERE deleted_at IS NULL;

-- 2. Transaction-hash uniqueness is already enforced.
--
-- Migration 023 creates idx_vault_transactions_transaction_hash_unique, and
-- 033 renames the column to transaction_hash with the index following it.
-- NULL hashes stay distinct there, which is the behaviour server-submitted
-- rows depend on. Re-adding it here would duplicate the index and, worse,
-- reference the pre-rename column name.

-- 3. Share accounting cannot go negative.
--
-- shares_minted_or_burned is stored as a magnitude; direction comes from
-- `type`. A negative value would silently invert a deposit into a withdrawal
-- when the ledger is summed.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'check_vault_tx_shares_non_negative'
    ) THEN
        ALTER TABLE vault_transactions
            ADD CONSTRAINT check_vault_tx_shares_non_negative
            CHECK (shares_minted_or_burned IS NULL OR shares_minted_or_burned >= 0);
    END IF;
END$$;

-- 4. A recorded share price must be positive.
--
-- Zero would make every conversion a division by zero; negative is
-- meaningless. Both indicate a computation bug worth failing loudly on.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'check_vault_tx_share_price_positive'
    ) THEN
        ALTER TABLE vault_transactions
            ADD CONSTRAINT check_vault_tx_share_price_positive
            CHECK (share_price_at_time IS NULL OR share_price_at_time > 0);
    END IF;
END$$;

-- 5. Yield and fees cannot go negative.
--
-- vaults already constrains total_deposited and current_balance; these two
-- were left unguarded.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'check_vaults_yield_earned_non_negative'
    ) THEN
        ALTER TABLE vaults
            ADD CONSTRAINT check_vaults_yield_earned_non_negative
            CHECK (yield_earned >= 0);
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'check_vaults_fees_paid_non_negative'
    ) THEN
        ALTER TABLE vaults
            ADD CONSTRAINT check_vaults_fees_paid_non_negative
            CHECK (fees_paid >= 0);
    END IF;
END$$;
