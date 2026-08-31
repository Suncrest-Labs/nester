-- Transactional outbox for webhook and notification side effects (#1049).
--
-- A side effect is inserted here *inside the same transaction as the domain
-- write that caused it*. That shared transaction is the whole mechanism:
-- if the domain write rolls back the intent to notify rolls back with it,
-- and if the domain write commits the intent is durable even when the
-- process dies one instruction later. Nothing about ordering the writes
-- carefully can achieve this; only the shared transaction can.
--
-- The relay (internal/domain/outbox/relay.go) then hands each row to the
-- durable job queue (migration 059) — retry, backoff, and dead-lettering
-- are the queue's, never re-implemented here, so there is exactly one
-- retry mechanism in the system rather than two that eventually disagree.
CREATE TABLE outbox (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),

    -- The aggregate this side effect belongs to. Ordering is guaranteed
    -- per-aggregate and never globally: the relay dispatches only the
    -- oldest undispatched row of each (aggregate_type, aggregate_id), so a
    -- stuck aggregate stalls itself and nothing else.
    aggregate_type  VARCHAR(100) NOT NULL,
    aggregate_id    TEXT         NOT NULL,

    -- The side-effect event, e.g. 'webhook.fanout' or 'notification.send'.
    -- outbox.Routes maps this to the job type the relay enqueues.
    event_type      VARCHAR(100) NOT NULL,
    payload         JSONB        NOT NULL DEFAULT '{}'::jsonb,

    -- Stable across every redelivery of this logical side effect and
    -- carried through to the consumer (webhook header + body, notification
    -- dedup key). Delivery is at-least-once, so this is what makes a
    -- consumer able to discard repeats. UNIQUE so a producer that retries
    -- its own transaction cannot enqueue the same side effect twice.
    dedupe_key      TEXT         NOT NULL UNIQUE,

    -- 'pending'     : waiting to be handed to the job queue
    -- 'dispatching' : handed over; a job row owns the delivery attempt
    -- 'dispatched'  : the job succeeded — terminal, prunable
    -- 'dead'        : poison — terminal, retained longer for diagnosis
    status          VARCHAR(20)  NOT NULL DEFAULT 'pending',

    -- Counts hand-offs to the job queue, NOT delivery attempts (the queue
    -- owns those). Normally 1; it only climbs when a hand-off itself fails
    -- — an enqueue error, or a job row that vanished — and max_attempts
    -- bounds that loop so a row can never churn forever.
    attempts        INT          NOT NULL DEFAULT 0,
    max_attempts    INT          NOT NULL DEFAULT 5,
    next_attempt_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    -- Claim lease, mirroring jobs.leased_until: a relay that dies between
    -- claiming a row and enqueueing its job leaves the row reclaimable once
    -- the lease lapses, with no separate reaper.
    leased_until    TIMESTAMPTZ,

    -- The queue job currently carrying this row's delivery. Set when the
    -- row enters 'dispatching'; the relay reconciles against it.
    job_id          UUID,

    last_error      TEXT,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    dispatched_at   TIMESTAMPTZ,

    CONSTRAINT outbox_status_check
        CHECK (status IN ('pending', 'dispatching', 'dispatched', 'dead')),
    CONSTRAINT outbox_attempts_nonneg CHECK (attempts >= 0),
    CONSTRAINT outbox_max_attempts_pos CHECK (max_attempts >= 1)
);

-- Claim hot path: find the per-aggregate head of the undispatched queue.
-- `created_at, id` is the intra-aggregate order the relay preserves (id
-- breaks ties for rows written inside the same transaction, which share a
-- transaction timestamp).
CREATE INDEX idx_outbox_undispatched
    ON outbox (aggregate_type, aggregate_id, created_at, id)
    WHERE status IN ('pending', 'dispatching');

-- Reconcile path: rows whose job needs its terminal state read back.
CREATE INDEX idx_outbox_dispatching_job
    ON outbox (job_id)
    WHERE status = 'dispatching' AND job_id IS NOT NULL;

-- Retention sweep: prune terminal rows by age.
CREATE INDEX idx_outbox_terminal_age
    ON outbox (status, updated_at)
    WHERE status IN ('dispatched', 'dead');
