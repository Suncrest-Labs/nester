ALTER TABLE apy_snapshots
    ADD COLUMN flagged BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN flag_reason TEXT;

CREATE INDEX idx_apy_snapshots_flagged ON apy_snapshots (protocol_slug, captured_at) WHERE flagged;
