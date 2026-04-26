CREATE TABLE IF NOT EXISTS event_indexer_state (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    last_indexed_ledger BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO event_indexer_state (id, last_indexed_ledger)
VALUES (1, 0)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS processed_chain_events (
    event_id TEXT PRIMARY KEY,
    contract_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    ledger BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
