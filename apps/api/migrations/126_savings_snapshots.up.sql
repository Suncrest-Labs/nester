-- Periodic snapshots of the savings vault contract state, used to derive the
-- realized/trailing savings rate. Values are the contract's i128 stroop
-- integers, hence numeric(38,0).
CREATE TABLE IF NOT EXISTS savings_snapshots (
    id             BIGSERIAL PRIMARY KEY,
    taken_at       TIMESTAMPTZ   NOT NULL,
    share_price    NUMERIC(38,0) NOT NULL,
    total_assets   NUMERIC(38,0) NOT NULL,
    total_supply   NUMERIC(38,0) NOT NULL,
    reserve_shares NUMERIC(38,0) NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_savings_snapshots_taken_at
    ON savings_snapshots (taken_at);
