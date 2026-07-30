-- No-op. This migration was an accidental byte-for-byte duplicate of
-- 010_update_users_table.up.sql (a campaign PR copied it as a template and never
-- changed the body). The user-table changes are already applied by 010; re-running
-- them here fails on a fresh database because `ADD COLUMN kyc_status` has no
-- IF NOT EXISTS guard. Neutralized to keep the version sequence intact.
-- See AUDIT_REPORT.md.
SELECT 1;
