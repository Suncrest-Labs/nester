-- Add unique constraint on (protocol_slug, captured_at) so that duplicate
-- oracle reports for the same protocol at the same timestamp are rejected
-- at the database level, making Upsert truly idempotent.
ALTER TABLE apy_snapshots
    ADD CONSTRAINT uq_apy_snapshots_protocol_time UNIQUE (protocol_slug, captured_at);
