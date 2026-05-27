CREATE TABLE IF NOT EXISTS event_indexer_state (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    last_indexed_ledger BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Cursor seeded at runtime from Stellar ledger tip; never insert 0 here.

CREATE TABLE IF NOT EXISTS processed_chain_events (
    event_id TEXT PRIMARY KEY,
    contract_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    ledger BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
