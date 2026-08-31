-- Durable chain-submission records (nester#1085).
--
-- Written BEFORE a transaction is handed to Soroban RPC, so a lost response
-- can never leave an on-chain transaction that nothing in the system knows
-- about. The reconciler resolves the outcome from the chain; nothing here is
-- ever resubmitted automatically.
--
-- A new table rather than an extension of chain_submissions (migration 099).
-- That table is an unwired scaffold whose shape does not fit this record: it
-- requires sequence_number and signed_envelope NOT NULL and is unique per
-- (account, sequence). This record deliberately stores neither — the signed
-- envelope carries signatures and operation arguments, and is never persisted
-- or logged (see internal/stellar/tracing.go), and nothing here resubmits an
-- old envelope, so there is nothing to keep it for. Reshaping 099 in place
-- would also break the tests written against it.
CREATE TABLE IF NOT EXISTS submission_intents (
    id                    UUID        PRIMARY KEY DEFAULT gen_random_uuid(),

    -- The caller-supplied identity of the logical operation. UNIQUE is the
    -- concurrency guarantee: N simultaneous duplicate requests contend on
    -- this index and exactly one wins, so an application-level
    -- "check then insert" race cannot produce two chain submissions.
    idempotency_reference TEXT        NOT NULL UNIQUE
                                       CHECK (char_length(idempotency_reference) > 0),

    -- The real Stellar transaction hash, computed from the signed envelope
    -- before submitting. This is the exact identity the reconciler asks the
    -- chain about — never a heuristic match on account, amount, or time.
    transaction_hash      TEXT        NOT NULL,

    -- The transaction's own signed maxTime. Once the chain's clock passes it
    -- the network must refuse the transaction, which is what turns a
    -- NOT_FOUND into proof that it can never land.
    valid_until           TIMESTAMPTZ NOT NULL,

    source_account        TEXT        NOT NULL,
    domain_action         TEXT        NOT NULL DEFAULT '',

    -- pending is not failure: it means the outcome is not yet known. The four
    -- terminal states are all chain-derived, except unresolvable, which
    -- records that the chain can no longer tell us and a human must decide.
    state                 TEXT        NOT NULL DEFAULT 'pending'
                                       CHECK (state IN ('pending', 'landed', 'rejected', 'expired', 'unresolvable')),

    attempt               INTEGER     NOT NULL DEFAULT 0,
    outcome_detail        TEXT        NOT NULL DEFAULT '',

    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    submitted_at          TIMESTAMPTZ,
    resolved_at           TIMESTAMPTZ,

    -- Reconciliation bookkeeping. next_reconcile_at spaces out repeated
    -- checks of a submission the chain has not yet decided, so an RPC outage
    -- does not turn into a tight polling loop.
    reconcile_attempts    INTEGER     NOT NULL DEFAULT 0,
    last_reconciled_at    TIMESTAMPTZ,
    next_reconcile_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The reconciler's only query: unresolved submissions that are due. Partial,
-- because resolved rows are the overwhelming majority over time and never
-- need to be found this way.
CREATE INDEX IF NOT EXISTS idx_submission_intents_due
    ON submission_intents (next_reconcile_at)
    WHERE state = 'pending';

-- Operator lookup: "what happened to this transaction on chain".
CREATE INDEX IF NOT EXISTS idx_submission_intents_hash
    ON submission_intents (transaction_hash);
