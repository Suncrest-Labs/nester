CREATE TABLE IF NOT EXISTS system_state (
    key        TEXT        PRIMARY KEY,
    value      TEXT        NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed the event-indexer cursor so the indexer can always do a plain Get.
INSERT INTO system_state (key, value)
VALUES ('event_indexer.last_ledger', '0')
ON CONFLICT (key) DO NOTHING;
