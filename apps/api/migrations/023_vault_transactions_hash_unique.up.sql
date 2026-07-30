-- Enforce a single ledger row per on-chain transaction hash so that confirmed
-- deposit/withdrawal balance changes can be applied idempotently (ON CONFLICT)
-- by the auto-confirmation worker. NULL hashes remain allowed and distinct
-- (Postgres treats NULLs as distinct in a unique index), so legacy rows without
-- a hash are unaffected.
-- NOTE: at this point in the chain the column is still named `tx_hash`; it is
-- renamed to `transaction_hash` later (033). The index follows the column through
-- the rename, so the end state is a unique index on `transaction_hash` as intended.
CREATE UNIQUE INDEX IF NOT EXISTS idx_vault_transactions_transaction_hash_unique
    ON vault_transactions (tx_hash);
