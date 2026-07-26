-- Durable async job queue (#824).
--
-- A single `jobs` table backs the whole queue. Runnable work is any row that is
-- `pending` and due (`next_run_at <= now`), or `running` with an expired lease
-- (`leased_until <= now`) — the latter is how a crashed worker's in-flight job
-- is reclaimed without a separate reaper. Terminal states are `succeeded` and
-- `dead`; `dead` rows are the dead-letter queue and are never dequeued again.
CREATE TABLE jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type            VARCHAR(100) NOT NULL,
    payload         JSONB        NOT NULL DEFAULT '{}'::jsonb,
    status          VARCHAR(20)  NOT NULL DEFAULT 'pending',
    priority        INT          NOT NULL DEFAULT 0,
    attempts        INT          NOT NULL DEFAULT 0,
    max_attempts    INT          NOT NULL DEFAULT 5,
    idempotency_key TEXT,
    correlation_id  TEXT,
    next_run_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    leased_until    TIMESTAMPTZ,
    last_error      TEXT,
    result          JSONB,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT jobs_status_check
        CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'dead')),
    CONSTRAINT jobs_attempts_nonneg CHECK (attempts >= 0),
    CONSTRAINT jobs_max_attempts_pos CHECK (max_attempts >= 1)
);

-- Dequeue hot path: fetch due `pending` rows highest-priority, oldest-first.
CREATE INDEX idx_jobs_pending_ready
    ON jobs (priority DESC, next_run_at)
    WHERE status = 'pending';

-- Lease-reclaim path: find `running` rows whose visibility timeout has lapsed.
CREATE INDEX idx_jobs_running_lease
    ON jobs (leased_until)
    WHERE status = 'running';

-- Per-type observability (queue depth / DLQ depth by type).
CREATE INDEX idx_jobs_type_status ON jobs (type, status);

-- Idempotent enqueue: a (type, idempotency_key) may exist at most once while the
-- job is still live. Terminal rows (`succeeded`, `dead`) are excluded so the same
-- key can be re-enqueued after the previous job fully resolved.
CREATE UNIQUE INDEX uq_jobs_idempotency_key
    ON jobs (type, idempotency_key)
    WHERE idempotency_key IS NOT NULL
      AND status IN ('pending', 'running');
