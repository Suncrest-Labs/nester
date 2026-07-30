-- No-op. This migration was a byte-for-byte duplicate of 033_update_vault_transactions
-- (a campaign PR copied it as a template). 033 already adds the columns and renames
-- tx_hash -> transaction_hash; re-running the rename here fails because tx_hash no
-- longer exists. Neutralized to keep the version sequence intact. See AUDIT_REPORT.md.
SELECT 1;
