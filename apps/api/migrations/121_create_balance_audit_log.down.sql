-- balance_audit_log is the append-only, indefinite-retention audit trail
-- documented in docs/balance-audit-trail.md — dropping it here would
-- permanently destroy that history, which a routine rollback must never do.
-- If an operator genuinely needs to remove this table (e.g. reverting a
-- migration that never should have shipped), that is a deliberate manual
-- operation, not something a `migrate down` should do silently.
--
-- We do, however, drop this table's foreign keys to vaults and users. Those
-- FKs are the only thing tying balance_audit_log's lifetime to those two
-- tables; leaving them in place would block an earlier migration (002's
-- `DROP TABLE vaults`, and ultimately 001's `DROP TABLE users`) from
-- completing during a full-chain rollback, with no benefit to the audit
-- trail itself — the rows and the table stay exactly as they are, just no
-- longer FK-constrained. A subsequent `migrate up` recreates both
-- constraints from this file's up.sql.
ALTER TABLE balance_audit_log DROP CONSTRAINT IF EXISTS balance_audit_log_vault_id_fkey;
ALTER TABLE balance_audit_log DROP CONSTRAINT IF EXISTS balance_audit_log_user_id_fkey;
