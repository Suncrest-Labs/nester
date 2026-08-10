CREATE TABLE risk_scores (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  vault_id UUID NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,
  overall_score NUMERIC(5,2) NOT NULL,
  confidence NUMERIC(5,2) NOT NULL,
  tier TEXT NOT NULL,
  factors JSONB NOT NULL,
  computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_risk_scores_vault_computed ON risk_scores(vault_id, computed_at DESC);
CREATE INDEX idx_risk_scores_vault_latest ON risk_scores(vault_id) INCLUDE (overall_score, confidence, tier, factors, computed_at);
