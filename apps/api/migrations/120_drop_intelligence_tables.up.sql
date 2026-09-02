-- The Prometheus intelligence service was removed from the product. These
-- tables backed AI-only features: the tool-invocation audit chain, the
-- generated financial digests, and the per-user digest cadence preference.
DROP TABLE IF EXISTS tool_invocations;
DROP TABLE IF EXISTS user_digests;
ALTER TABLE notification_preferences DROP COLUMN IF EXISTS digest_cadence;
