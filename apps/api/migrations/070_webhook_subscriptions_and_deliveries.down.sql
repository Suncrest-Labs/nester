DROP TABLE IF EXISTS webhook_deliveries;

DROP INDEX IF EXISTS idx_webhooks_status;

ALTER TABLE webhooks
    DROP COLUMN IF EXISTS event_types,
    DROP COLUMN IF EXISTS secret_key_version,
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS consecutive_dead_letters,
    DROP COLUMN IF EXISTS suspended_at;
