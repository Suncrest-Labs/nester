CREATE TABLE IF NOT EXISTS reconciliation_runs (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    level           TEXT        NOT NULL CHECK (level IN ('balance', 'transaction', 'invariant')),
    comparator      TEXT        NOT NULL,
    status          TEXT        NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
    scope           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    checked_count   INTEGER     NOT NULL DEFAULT 0,
    finding_count   INTEGER     NOT NULL DEFAULT 0,
    critical_count  INTEGER     NOT NULL DEFAULT 0,
    checkpoint_key  TEXT,
    checkpoint_from TEXT,
    checkpoint_to   TEXT,
    error           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_reconciliation_runs_level_started
    ON reconciliation_runs (level, started_at DESC);

CREATE TABLE IF NOT EXISTS reconciliation_findings (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id           UUID        NOT NULL REFERENCES reconciliation_runs(id) ON DELETE CASCADE,
    level            TEXT        NOT NULL CHECK (level IN ('balance', 'transaction', 'invariant')),
    type             TEXT        NOT NULL CHECK (type IN ('missing', 'extra', 'mismatch', 'stuck')),
    severity         TEXT        NOT NULL CHECK (severity IN ('informational', 'warning', 'critical')),
    entity_type      TEXT        NOT NULL,
    entity_id        TEXT        NOT NULL,
    recorded_value   NUMERIC(38, 18),
    on_chain_value   NUMERIC(38, 18),
    difference       NUMERIC(38, 18),
    tolerance        NUMERIC(38, 18) NOT NULL DEFAULT 0,
    observed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    details          JSONB       NOT NULL DEFAULT '{}'::jsonb,
    resolution_state TEXT        NOT NULL DEFAULT 'open' CHECK (resolution_state IN ('open', 'reviewing', 'resolved')),
    resolution_note  TEXT,
    resolved_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (run_id, level, type, entity_type, entity_id)
);

CREATE INDEX IF NOT EXISTS idx_reconciliation_findings_open_severity
    ON reconciliation_findings (resolution_state, severity, observed_at DESC);

CREATE INDEX IF NOT EXISTS idx_reconciliation_findings_entity
    ON reconciliation_findings (entity_type, entity_id, observed_at DESC);

CREATE TABLE IF NOT EXISTS reconciliation_checkpoints (
    key        TEXT        PRIMARY KEY,
    value      TEXT        NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
