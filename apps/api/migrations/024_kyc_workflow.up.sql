ALTER TABLE users ADD COLUMN IF NOT EXISTS kyc_submitted_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS kyc_reviewed_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS kyc_rejection_reason TEXT;
CREATE TABLE IF NOT EXISTS kyc_documents (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id),
  id_type TEXT NOT NULL,
  id_number TEXT NOT NULL,
  front_object_key TEXT NOT NULL,
  back_object_key TEXT,
  submitted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
