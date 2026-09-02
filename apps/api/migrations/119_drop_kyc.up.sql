-- KYC existed only to gate fiat payouts; with the offramp removed (migration
-- 118) nothing reads or writes KYC state. Dropping the table and columns
-- removes the last at-rest copies of identity-document PII.
DROP TABLE IF EXISTS kyc_documents;
ALTER TABLE users
    DROP COLUMN IF EXISTS kyc_status,
    DROP COLUMN IF EXISTS kyc_submitted_at,
    DROP COLUMN IF EXISTS kyc_reviewed_at,
    DROP COLUMN IF EXISTS kyc_rejection_reason;
