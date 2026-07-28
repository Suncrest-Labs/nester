-- Historical chain backfill and resync tool (nester#840): a backfill_runs
-- row is the checkpoint for one operator-initiated run over a ledger range.
-- Progress is persisted after each processed batch so a crash resumes from
-- last_ledger_done rather than restarting the whole range.
CREATE TABLE IF NOT EXISTS backfill_runs (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    from_ledger       BIGINT      NOT NULL,
    to_ledger         BIGINT      NOT NULL CHECK (to_ledger >= from_ledger),
    -- Empty means "all vault contracts", matching the forward indexer's
    -- loadVaultContractIDs default.
    contract_ids      TEXT[]      NOT NULL DEFAULT '{}',
    -- 'backfill' processes never-before-seen ranges relying on dedup alone;
    -- 'rebuild' additionally clears append-only derived rows + their
    -- processed_events entries in range before reprocessing (see
    -- resettableEventTypes in backfill.go for which event types this is
    -- offered for — deposit/withdraw are refused because vaults.
    -- total_deposited/current_balance are incremental, not idempotent
    -- absolute writes, so reprocessing them after a reset would double-count).
    mode              TEXT        NOT NULL DEFAULT 'backfill'
                                   CHECK (mode IN ('backfill', 'rebuild')),
    dry_run           BOOLEAN     NOT NULL DEFAULT FALSE,
    status            TEXT        NOT NULL DEFAULT 'running'
                                   CHECK (status IN ('running', 'completed', 'failed', 'cancelled')),
    -- Resumability checkpoint: the last ledger whose events were fully
    -- applied and committed. A resumed run starts from last_ledger_done + 1.
    last_ledger_done  BIGINT,
    events_processed  BIGINT      NOT NULL DEFAULT 0,
    events_skipped_duplicate BIGINT NOT NULL DEFAULT 0,
    last_error        TEXT,
    -- Who initiated it (#840's "operator-initiated ... audited" requirement).
    initiated_by      TEXT        NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_backfill_runs_status ON backfill_runs (status);
