-- Per-category channel preference overrides (nester#829). Categories are
-- "safety" | "transactional" | "promotional"; safety notifications always
-- bypass this table (and preferences generally) at the application layer, so
-- no row for "safety" is ever expected to matter, but nothing stops one from
-- being stored for forward-compatibility.
--
-- Stored as JSONB rather than per-category columns to avoid a 3-category x
-- 3-channel column explosion; shape is:
--   {"promotional": {"email": false, "websocket": true, "push": false}, ...}
-- A category missing from the JSON (the common case — most users never touch
-- this) falls back to notifications.DefaultPreferencesForCategory at the
-- application layer, NOT to a SQL default, since that logic already exists in
-- Go and duplicating it in SQL would be a second source of truth.
ALTER TABLE notification_preferences
    ADD COLUMN IF NOT EXISTS category_overrides JSONB NOT NULL DEFAULT '{}'::jsonb;
