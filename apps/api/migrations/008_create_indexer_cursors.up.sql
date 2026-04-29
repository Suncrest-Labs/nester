CREATE TABLE IF NOT EXISTS indexer_cursors (
    id TEXT PRIMARY KEY,
    last_ledger BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO indexer_cursors (id, last_ledger) VALUES ('default', 0) ON CONFLICT DO NOTHING;
