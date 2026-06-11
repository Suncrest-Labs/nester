DROP TABLE IF EXISTS kyc_documents;
ALTER TABLE users DROP COLUMN IF EXISTS kyc_rejection_reason;
ALTER TABLE users DROP COLUMN IF EXISTS kyc_reviewed_at;
ALTER TABLE users DROP COLUMN IF EXISTS kyc_submitted_at;
