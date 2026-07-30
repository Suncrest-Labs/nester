-- Idempotency-Key middleware storage (nester#835). A client-supplied key is
-- claimed atomically via INSERT ... ON CONFLICT DO NOTHING: the first
-- request for a (user, key) pair wins the claim and executes the handler;
-- any concurrent or later request with the same key sees the conflict and
-- either waits for completion (still in_progress) or gets the stored
-- response back (completed) — see internal/middleware/idempotency.go.
--
-- Deliberately Postgres-only (no Redis lock): INSERT ... ON CONFLICT is
-- already atomic across processes/instances, so a separate cross-process
-- lock would be redundant complexity, not a correctness requirement. This
-- also directly satisfies "completed keys must be durably persisted, not
-- Redis-only" from the issue by construction.
CREATE TABLE IF NOT EXISTS idempotency_keys (
    user_id             UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key                 TEXT        NOT NULL CHECK (char_length(key) > 0),
    -- SHA-256 hex of method+path+body, so a key reused with a materially
    -- different request is detected and rejected rather than silently
    -- returning an unrelated stored response (#835's fingerprinting guard).
    request_fingerprint TEXT        NOT NULL,
    status              TEXT        NOT NULL DEFAULT 'in_progress'
                                     CHECK (status IN ('in_progress', 'completed')),
    response_status     INT,
    response_body       BYTEA,
    response_content_type TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at        TIMESTAMPTZ,
    -- Bounds how long a retry is honored; a purge job removes rows past
    -- this so the table doesn't grow unbounded (#835's TTL requirement).
    expires_at          TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, key)
);

CREATE INDEX IF NOT EXISTS idx_idempotency_keys_expires_at ON idempotency_keys (expires_at);
