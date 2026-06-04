CREATE TABLE vault_risk_snapshots (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  vault_id UUID NOT NULL REFERENCES vaults(id),
  overall_score NUMERIC(5,2) NOT NULL,
  tier TEXT NOT NULL,
  computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ON vault_risk_snapshots(vault_id, computed_at DESC);