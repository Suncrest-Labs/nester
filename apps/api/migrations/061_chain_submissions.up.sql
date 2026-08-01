CREATE TABLE chain_submissions (
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

CREATE UNIQUE INDEX idx_chain_submissions_account_seq ON chain_submissions(source_account, sequence_number);
CREATE INDEX idx_chain_submissions_hash ON chain_submissions(transaction_hash);
CREATE INDEX idx_chain_submissions_status ON chain_submissions(status) WHERE status IN ('pending', 'submitted', 'unknown');
CREATE INDEX idx_chain_submissions_created ON chain_submissions(created_at DESC);

CREATE TABLE account_sequences (
  source_account TEXT PRIMARY KEY,
  next_sequence BIGINT NOT NULL,
  last_synced_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
