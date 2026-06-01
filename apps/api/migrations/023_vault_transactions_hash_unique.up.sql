-- Enforce a single ledger row per on-chain transaction hash so that confirmed
-- deposit/withdrawal balance changes can be applied idempotently (ON CONFLICT)
-- by the auto-confirmation worker. NULL hashes remain allowed and distinct
-- (Postgres treats NULLs as distinct in a unique index), so legacy rows without
-- a hash are unaffected.
CREATE UNIQUE INDEX IF NOT EXISTS idx_vault_transactions_transaction_hash_unique
    ON vault_transactions (transaction_hash);
