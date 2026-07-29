-- Unified time-series store for APY, TVL, and portfolio history.
-- Existing snapshot tables remain in place; producers can backfill and dual-write
-- into this store without breaking current API responses.
CREATE TABLE IF NOT EXISTS timeseries_raw (
    series_key   TEXT        NOT NULL,
    metric       TEXT        NOT NULL CHECK (metric IN ('apy', 'tvl', 'portfolio')),
    entity_type  TEXT        NOT NULL,
    entity_id    TEXT        NOT NULL,
    observed_at  TIMESTAMPTZ NOT NULL,
    value        NUMERIC(38, 18) NOT NULL,
    dimensions   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (series_key, observed_at)
) PARTITION BY RANGE (observed_at);

CREATE TABLE IF NOT EXISTS timeseries_raw_default
    PARTITION OF timeseries_raw DEFAULT;

CREATE INDEX IF NOT EXISTS idx_timeseries_raw_series_observed_at
    ON timeseries_raw (series_key, observed_at DESC);

CREATE INDEX IF NOT EXISTS idx_timeseries_raw_metric_entity_time
    ON timeseries_raw (metric, entity_type, entity_id, observed_at DESC);

CREATE TABLE IF NOT EXISTS timeseries_rollups (
    series_key   TEXT        NOT NULL,
    metric       TEXT        NOT NULL CHECK (metric IN ('apy', 'tvl', 'portfolio')),
    entity_type  TEXT        NOT NULL,
    entity_id    TEXT        NOT NULL,
    resolution   TEXT        NOT NULL CHECK (resolution IN ('minute', 'hour', 'day')),
    bucket_start TIMESTAMPTZ NOT NULL,
    open         NUMERIC(38, 18) NOT NULL,
    high         NUMERIC(38, 18) NOT NULL,
    low          NUMERIC(38, 18) NOT NULL,
    close        NUMERIC(38, 18) NOT NULL,
    average      NUMERIC(38, 18) NOT NULL,
    last         NUMERIC(38, 18) NOT NULL,
    point_count  BIGINT      NOT NULL CHECK (point_count > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (series_key, resolution, bucket_start)
);

CREATE INDEX IF NOT EXISTS idx_timeseries_rollups_metric_entity_time
    ON timeseries_rollups (metric, entity_type, entity_id, resolution, bucket_start DESC);

CREATE TABLE IF NOT EXISTS timeseries_rollup_checkpoints (
    resolution      TEXT        PRIMARY KEY CHECK (resolution IN ('minute', 'hour', 'day')),
    processed_until TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
