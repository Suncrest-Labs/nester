-- No-op. Paired with the neutralized 020_update_users_table.up.sql (a duplicate of
-- 010). The real rollback logic lives in 010_update_users_table.down.sql; repeating
-- it here would fail (e.g. re-adding the already-present email column).
SELECT 1;
