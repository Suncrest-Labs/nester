-- Fair-ordering emergency withdrawal queue (issue #814).
--
-- One row per queue entry, keyed by the on-chain (contract, seq) pair so a
-- user can see their position without an RPC round-trip on every page load.
-- `seq` is the contract's monotonic sequence number — ordering here must
-- always match on-chain ordering, never be re-derived from `enqueued_at`.
CREATE TABLE IF NOT EXISTS emergency_withdrawal_queue (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vault_contract_address TEXT NOT NULL CHECK (char_length(vault_contract_address) > 0),
    user_address       TEXT NOT NULL CHECK (char_length(user_address) > 0),
    seq                BIGINT NOT NULL CHECK (seq > 0),
    shares_requested   NUMERIC(38,0) NOT NULL CHECK (shares_requested > 0),
    shares_filled      NUMERIC(38,0) NOT NULL DEFAULT 0 CHECK (shares_filled >= 0),
    status             TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'filled', 'cancelled')),
    enqueued_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_emergency_withdrawal_queue_vault_seq UNIQUE (vault_contract_address, seq)
);

CREATE INDEX IF NOT EXISTS idx_emergency_withdrawal_queue_user
    ON emergency_withdrawal_queue (vault_contract_address, user_address)
    WHERE status = 'open';

CREATE INDEX IF NOT EXISTS idx_emergency_withdrawal_queue_status
    ON emergency_withdrawal_queue (vault_contract_address, status);
