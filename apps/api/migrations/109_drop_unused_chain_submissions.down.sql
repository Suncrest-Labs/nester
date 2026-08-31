-- Restore the submission scaffold exactly as 099_chain_submissions left it, so
-- rolling back to a release that still shipped SubmissionPipeline finds the
-- objects it expects. Both tables were empty when 109 dropped them, so there
-- is no data to restore.

CREATE TABLE IF NOT EXISTS chain_submissions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  source_account TEXT NOT NULL,
  sequence_number BIGINT NOT NULL,
  transaction_hash TEXT NOT NULL,
  signed_envelope TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  job_id UUID,
  domain_action TEXT,
  submitted_at TIMESTAMPTZ,
  confirmed_at TIMESTAMPTZ,
  error_message TEXT,
  retry_count INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_chain_submissions_account_seq ON chain_submissions(source_account, sequence_number);
CREATE INDEX IF NOT EXISTS idx_chain_submissions_hash ON chain_submissions(transaction_hash);
CREATE INDEX IF NOT EXISTS idx_chain_submissions_status ON chain_submissions(status) WHERE status IN ('pending', 'submitted', 'unknown');
CREATE INDEX IF NOT EXISTS idx_chain_submissions_created ON chain_submissions(created_at DESC);

CREATE TABLE IF NOT EXISTS account_sequences (
  source_account TEXT PRIMARY KEY,
  next_sequence BIGINT NOT NULL,
  last_synced_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
