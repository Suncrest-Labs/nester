-- Reverses 107_add_kyc_identity_fields.up.sql (nester#1190).
ALTER TABLE kyc_documents
    DROP COLUMN IF EXISTS full_name,
    DROP COLUMN IF EXISTS date_of_birth,
    DROP COLUMN IF EXISTS country;
