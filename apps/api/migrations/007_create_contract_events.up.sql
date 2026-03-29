CREATE TABLE IF NOT EXISTS contract_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    contract_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    ledger BIGINT NOT NULL,
    tx_hash TEXT NOT NULL,
    data JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_contract_events_contract_id_ledger ON contract_events (contract_id, ledger);
CREATE INDEX idx_contract_events_event_type ON contract_events (event_type);
