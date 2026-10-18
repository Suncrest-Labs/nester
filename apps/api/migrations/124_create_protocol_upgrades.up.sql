-- Track timelocked contract upgrade proposals (PROP_UPG, CAN_UPG, EXEC_UPG) — issue #1549.
CREATE TABLE IF NOT EXISTS protocol_upgrades (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    contract_address TEXT        NOT NULL,
    wasm_hash        TEXT        NOT NULL,
    eta              BIGINT      NOT NULL,
    proposer         TEXT        NOT NULL DEFAULT '',
    status           TEXT        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'cancelled', 'executed')),
    proposed_ledger  BIGINT      NOT NULL DEFAULT 0,
    tx_hash          TEXT        NOT NULL DEFAULT '',
    cancelled_by     TEXT,
    executed_by      TEXT,
    executed_at_ts   BIGINT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (contract_address, wasm_hash)
);

CREATE INDEX IF NOT EXISTS idx_protocol_upgrades_status_eta
    ON protocol_upgrades (status, eta ASC);
