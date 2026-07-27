"""Risk-adjusted yield scoring for the AI rebalancing engine.

Scores each supported Stellar DeFi protocol on a Sharpe-ratio inspired basis so
the rebalance engine can rank candidate allocations by risk-adjusted return
rather than raw APY alone.
"""

from dataclasses import dataclass

# Annualised return assumed available risk-free (e.g. a stablecoin-only vault
# tier), expressed as a percentage-point APY to match how APYs are represented
# everywhere else in this module (4.0 means 4%, not 0.04). Used as the
# baseline an allocation must beat to be worth the risk.
RISK_FREE_RATE = 2.0

# Floor applied to the risk_adjusted_score denominator so a protocol with a
# perfect audit/TVL/age score and full liquidity never divides by zero.
_MIN_DENOMINATOR = 0.01


@dataclass(frozen=True)
class ProtocolRiskFactors:
    """Raw risk inputs for a single protocol, each on a 0-1 scale."""

    audit_score: float  # 1.0 = fully audited by reputable firms
    tvl_score: float  # 1.0 = large, stable TVL
    age_score: float  # 1.0 = long track record
    volatility_score: float  # 1.0 = highly volatile historical APY
    liquidity_score: float  # 1.0 = fully liquid / instant withdrawal


# Reference risk factors for protocols Nester vaults currently allocate to.
# Values are curated estimates; update as protocols mature or new audits land.
PROTOCOL_RISK_FACTORS: dict[str, ProtocolRiskFactors] = {
    "blend": ProtocolRiskFactors(
        audit_score=0.9, tvl_score=0.8, age_score=0.7, volatility_score=0.2, liquidity_score=0.9
    ),
    "lobstr": ProtocolRiskFactors(
        audit_score=0.75, tvl_score=0.6, age_score=0.6, volatility_score=0.35, liquidity_score=0.8
    ),
    "ultra": ProtocolRiskFactors(
        audit_score=0.7, tvl_score=0.5, age_score=0.4, volatility_score=0.45, liquidity_score=0.7
    ),
    "aquarius": ProtocolRiskFactors(
        audit_score=0.8, tvl_score=0.65, age_score=0.6, volatility_score=0.3, liquidity_score=0.85
    ),
    "fxdao": ProtocolRiskFactors(
        audit_score=0.65, tvl_score=0.4, age_score=0.3, volatility_score=0.55, liquidity_score=0.6
    ),
}

# Default factors for a protocol Nester has not yet risk-rated. Deliberately
# conservative so unknown protocols never outrank rated ones on data alone.
_DEFAULT_RISK_FACTORS = ProtocolRiskFactors(
    audit_score=0.4, tvl_score=0.4, age_score=0.3, volatility_score=0.6, liquidity_score=0.5
)

# Last-known APY per protocol (percentage points), used when DeFiLlama is
# unreachable or has no live pool for a protocol. Mirrors the issue's
# "fallback: cached last-known values" data source. Update periodically.
BASELINE_APY: dict[str, float] = {
    "blend": 8.4,
    "lobstr": 6.5,
    "ultra": 4.0,
    "aquarius": 9.0,
    "fxdao": 12.0,
}


def get_risk_factors(protocol: str) -> ProtocolRiskFactors:
    """Return the risk factors for `protocol`, falling back to conservative defaults."""
    return PROTOCOL_RISK_FACTORS.get(protocol.strip().lower(), _DEFAULT_RISK_FACTORS)


def composite_risk_score(factors: ProtocolRiskFactors) -> float:
    """Combine audit/TVL/age/volatility into a single 0-1 risk score (higher = riskier)."""
    inverse_safety = (
        (1 - factors.audit_score) + (1 - factors.tvl_score) + (1 - factors.age_score)
    ) / 3
    risk = (inverse_safety + factors.volatility_score) / 2
    return min(max(risk, 0.0), 1.0)


def risk_adjusted_score(apy: float, risk_score: float, liquidity_score: float) -> float:
    """Sharpe-ratio inspired risk-adjusted return.

    Mirrors the issue's reference formula: excess return over the risk-free
    rate, penalised by both protocol risk and illiquidity. The denominator is
    floored so a protocol with zero risk and full liquidity does not blow up
    to infinity.
    """
    denominator = max(risk_score * (1 - liquidity_score), _MIN_DENOMINATOR)
    return (apy - RISK_FREE_RATE) / denominator


def score_protocol(protocol: str, apy: float) -> float:
    """Convenience wrapper: look up risk factors for `protocol` and score `apy`."""
    factors = get_risk_factors(protocol)
    risk = composite_risk_score(factors)
    return risk_adjusted_score(apy, risk, factors.liquidity_score)
