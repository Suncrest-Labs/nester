-- Predictive protocol-health deterioration monitoring (nester#857):
-- deterioration_actions is the audit trail for every action taken in
-- response to a deterioration assessment — informational (ceiling cut,
-- recommended rebalance) and automatic capital movement alike. #857 is
-- explicit that automatic capital movement must never be silent, so every
-- row here is written before/alongside the corresponding notification.
CREATE TABLE IF NOT EXISTS deterioration_actions (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    protocol_slug TEXT        NOT NULL CHECK (char_length(protocol_slug) > 0),
    level         TEXT        NOT NULL CHECK (level IN ('mild', 'moderate', 'severe')),
    probability   DOUBLE PRECISION NOT NULL CHECK (probability >= 0 AND probability <= 1),
    kind          TEXT        NOT NULL CHECK (kind IN ('ceiling_cut', 'recommend_rebalance', 'automatic_rebalance')),
    -- vault_id is set only for automatic_rebalance (the specific vault
    -- moved); ceiling cuts and recommendations are protocol-wide.
    vault_id      UUID        REFERENCES vaults(id) ON DELETE SET NULL,
    -- rebalance_id references the vault_rebalances row the automatic action
    -- created, when applicable — the existing slippage-safe, auditable
    -- rebalance mechanism this feature bounds itself to.
    rebalance_id  UUID        REFERENCES vault_rebalances(id) ON DELETE SET NULL,
    explanation   TEXT        NOT NULL,
    error         TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_deterioration_actions_protocol ON deterioration_actions (protocol_slug, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_deterioration_actions_vault ON deterioration_actions (vault_id) WHERE vault_id IS NOT NULL;

-- deterioration_assessments retains scored history (not just triggered
-- actions) so calibration can later be checked against what actually
-- happened to a protocol afterward (#857's calibration-validation
-- requirement) — every tick's assessment is recorded, not only the ones
-- that crossed an alert threshold.
CREATE TABLE IF NOT EXISTS deterioration_assessments (
    id                          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    protocol_slug               TEXT        NOT NULL CHECK (char_length(protocol_slug) > 0),
    probability                 DOUBLE PRECISION NOT NULL CHECK (probability >= 0 AND probability <= 1),
    level                       TEXT        NOT NULL CHECK (level IN ('none', 'mild', 'moderate', 'severe')),
    tvl_outflow_velocity_pct    DOUBLE PRECISION NOT NULL,
    apy_abnormality_z_score     DOUBLE PRECISION NOT NULL,
    reported_vs_derived_gap_pct DOUBLE PRECISION NOT NULL,
    price_instability           DOUBLE PRECISION NOT NULL,
    sample_count                INT         NOT NULL,
    assessed_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_deterioration_assessments_protocol ON deterioration_assessments (protocol_slug, assessed_at DESC);
