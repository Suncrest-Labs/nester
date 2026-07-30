-- Feature flags and runtime configuration (issue #838).
-- Operational flags and non-secret tunables ONLY: secrets stay in the
-- environment / secret store. The application layer enforces a guard that
-- rejects secret-marked names before they reach this table.
CREATE TABLE IF NOT EXISTS feature_flags (
    name         TEXT PRIMARY KEY,
    type         TEXT NOT NULL CHECK (type IN ('bool', 'percentage', 'cohort', 'value')),
    enabled      BOOLEAN NOT NULL DEFAULT FALSE,
    percentage   DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (percentage >= 0 AND percentage <= 100),
    cohort       JSONB NOT NULL DEFAULT '[]'::jsonb,
    value        TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    owner        TEXT NOT NULL DEFAULT '',
    -- The value a boolean flag evaluates to when the flag service is
    -- unavailable. Kill switches keep the default FALSE: fail closed.
    fail_safe_on BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
