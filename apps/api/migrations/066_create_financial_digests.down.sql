DROP TABLE IF EXISTS user_digests;

ALTER TABLE notification_preferences
    DROP COLUMN IF EXISTS digest_cadence;
