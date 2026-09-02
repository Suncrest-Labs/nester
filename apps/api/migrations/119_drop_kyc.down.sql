-- Best-effort recreation of the KYC schema as it stood when dropped
-- (010/032/069/070/107 combined). Data is not restored.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS kyc_status TEXT NOT NULL DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS kyc_submitted_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS kyc_reviewed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS kyc_rejection_reason TEXT;

CREATE TABLE IF NOT EXISTS kyc_documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    id_type TEXT NOT NULL,
    id_number TEXT NOT NULL,
    front_object_key TEXT NOT NULL,
    back_object_key TEXT,
    full_name TEXT,
    date_of_birth DATE,
    country TEXT,
    id_number_encrypted BYTEA,
    id_number_fingerprint TEXT,
    front_object_key_encrypted BYTEA,
    back_object_key_encrypted BYTEA,
    key_version VARCHAR(32) NOT NULL DEFAULT 'v1',
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_kyc_documents_id_number_fingerprint
    ON kyc_documents (id_number_fingerprint)
    WHERE id_number_fingerprint IS NOT NULL;
