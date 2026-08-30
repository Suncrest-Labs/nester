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

-- Opening-balance entries for vaults that already existed (and may already
-- carry a nonzero balance) at the moment this migration runs. Without this,
-- balanceaudit.Reconcile — which sums balance_after - balance_before across
-- every entry starting from zero — would omit whatever balance a vault
-- already had before the audit trail started recording, making
-- reconciliation fail for every pre-existing vault with a nonzero balance.
-- One immutable row per vault: before=0, after=current_balance, actor is a
-- fixed system label (not a real user), same convention as
-- balanceaudit.SystemActor. created_at is left to the column default (NOW())
-- rather than backdated to v.created_at: the vault's balance may have
-- changed many times between vault creation and this migration running, none
-- of which is recorded, so stamping the vault's creation time on an entry
-- that actually reflects the balance as of the migration run would be
-- misleading.
INSERT INTO balance_audit_log (
    vault_id, user_id, actor, operation, amount, balance_before, balance_after
)
SELECT
    v.id,
    v.user_id,
    'system:migration',
    'opening_balance',
    v.current_balance,
    0,
    v.current_balance
FROM vaults v
WHERE NOT EXISTS (
    SELECT 1 FROM balance_audit_log b WHERE b.vault_id = v.id
);
