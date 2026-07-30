-- No-op. Accidental duplicate of 012_add_user_roles.up.sql. The user_roles table is
-- already created by 012 (and this copy only survived because it used
-- CREATE TABLE IF NOT EXISTS). Neutralized to remove the redundant migration.
-- See AUDIT_REPORT.md.
SELECT 1;
