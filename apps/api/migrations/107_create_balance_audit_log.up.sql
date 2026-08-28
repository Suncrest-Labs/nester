-- Append-only audit trail for every balance-changing vault operation
-- (nester#1124). Deliberately separate from audit_logs (migration 011/097):
-- audit_logs is the tamper-evident, hash-chained log for security/admin
-- actions across the whole system; this table is a narrow, purpose-built
-- ledger of just balance transitions, with explicit before/after columns so
-- a user's balance history can be reconstructed and reconciled without
-- parsing a JSONB detail blob.
--
-- No application code path updates or deletes rows in this table — see
-- internal/domain/balanceaudit. Retention: rows are kept indefinitely for
-- the life of the product; this table's growth is bounded by transaction
-- volume (one row per deposit/withdrawal/harvest/rebalance leg), which is
-- the same growth rate as vault_transactions already sustains.
CREATE TABLE IF NOT EXISTS balance_audit_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vault_id UUID NOT NULL REFERENCES vaults(id),
    user_id UUID NOT NULL REFERENCES users(id),
    -- actor: who/what caused the change. The owning user's id as text for a
    -- user-initiated action, or a fixed system label ("system:harvest",
    -- "system:rebalancer") for a background job. Kept as text (not a FK) so
    -- system actors don't need a synthetic user row.
    actor TEXT NOT NULL,
    operation TEXT NOT NULL,
    amount NUMERIC(48, 8) NOT NULL,
    balance_before NUMERIC(48, 8) NOT NULL,
    balance_after NUMERIC(48, 8) NOT NULL,
    -- chain_reference: the on-chain transaction hash this balance change
    -- corresponds to, when one exists. Nullable because some
    -- balance-affecting bookkeeping (e.g. fee accrual) may not have its own
    -- discrete transaction.
    chain_reference TEXT,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_balance_audit_log_vault_id ON balance_audit_log(vault_id, created_at);
CREATE INDEX IF NOT EXISTS idx_balance_audit_log_user_id ON balance_audit_log(user_id, created_at);

COMMENT ON TABLE balance_audit_log IS 'Append-only ledger of every balance-changing vault operation (nester#1124). No UPDATE/DELETE from application code.';
