DROP INDEX IF EXISTS idx_savings_gamification_events_user_time;
DROP TABLE IF EXISTS savings_achievements;
DROP TABLE IF EXISTS savings_gamification_events;
DROP TABLE IF EXISTS savings_gamification_state;
ALTER TABLE users DROP COLUMN IF EXISTS timezone;
