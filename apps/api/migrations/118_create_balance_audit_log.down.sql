-- No-op. balance_audit_log is the append-only, indefinite-retention audit
-- trail documented in docs/balance-audit-trail.md — dropping it here would
-- permanently destroy that history, which a routine rollback must never do.
-- If an operator genuinely needs to remove this table (e.g. reverting a
-- migration that never should have shipped), that is a deliberate manual
-- operation, not something a `migrate down` should do silently.
SELECT 1;
