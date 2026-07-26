"""Data models for the yield optimization engine (#848).

These models are the shared contract between:

- The pure, deterministic optimizer (`app.services.yield_optimizer`), which
  consumes `OptimizationRequest`/`YieldSourceInput`/`AllocationConstraints`
  and produces an `OptimizationResult`.
- The LLM explanation layer (`app.services.yield_explanation`), which reads an
  `OptimizationResult` and produces plain-language prose about it, but never
  computes or alters any number in it.
- The HTTP API (`app.routers.optimize`).
"""

from typing import Literal

from pydantic import BaseModel, Field

SourceStatus = Literal["active", "paused", "degraded", "closed"]


class YieldSourceInput(BaseModel):
    """One candidate yield source (a vault, pool, or protocol allocation slot)
    the optimizer may allocate capital to.

    Fields map to what `vault_context.py`'s `fetch_available_vaults` and
    `fetch_vault_risk` already return, plus a handful of allocation-relevant
    attributes (lock horizon, liquidity, protocol cap, status) that those
    fetchers do not yet expose but which the issue requires as hard
    constraints. Callers assemble this model from whatever combination of live
    API data and vault metadata they have; the optimizer only reads these
    fields and never re-fetches anything itself.
    """

    id: str = Field(..., min_length=1)
    protocol: str = Field(..., min_length=1, description="Protocol/vault display name")
    apy_pct: float = Field(..., ge=0, description="Expected APY, in percent (e.g. 8.5 = 8.5%)")
    risk_score: float = Field(
        ..., ge=0, le=100, description="Overall risk score (0-100) from the risk-scoring engine"
    )
    lock_days: int = Field(
        default=0, ge=0, description="Days capital is locked before it can be withdrawn"
    )
    liquid_fraction: float = Field(
        default=1.0,
        ge=0,
        le=1,
        description=(
            "Fraction of a position in this source that is immediately withdrawable "
            "(1.0 for fully-liquid/flexible sources, 0.0 for fully time-locked ones)"
        ),
    )
    status: SourceStatus = Field(
        default="active",
        description="Operational status; only 'active' sources are eligible",
    )
    deposit_cap_usd: float | None = Field(
        default=None,
        ge=0,
        description="Protocol/vault deposit cap in USD, if any. None means uncapped.",
    )
    current_balance_usd: float = Field(
        default=0.0,
        ge=0,
        description="Amount already deposited in this source, counted against its cap",
    )


class AllocationConstraints(BaseModel):
    """Hard constraints the optimizer must satisfy exactly, or report infeasible.

    None of these are ever silently relaxed: if the request's constraints
    cannot jointly be satisfied by the eligible sources, `optimize()` returns
    `OptimizationResult(feasible=False, ...)` with human-readable reasons
    instead of returning a "best effort" allocation that violates a limit.
    """

    max_per_source_weight: float = Field(
        default=0.4,
        gt=0,
        le=1,
        description="Diversification cap: max fraction of capital in any single source",
    )
    min_liquid_fraction: float = Field(
        default=0.0,
        ge=0,
        le=1,
        description="Minimum fraction of the total allocation that must be immediately liquid",
    )
    max_lock_days: int = Field(
        default=365 * 10,
        ge=0,
        description="Sources whose lock_days exceeds this are ineligible (lock-horizon fit)",
    )
    max_risk_score: float = Field(
        default=100.0,
        ge=0,
        le=100,
        description="Sources whose risk_score exceeds this are ineligible (risk tolerance)",
    )
    risk_aversion: float = Field(
        default=0.5,
        ge=0,
        description="Weight (gamma) on risk penalty in the objective; see yield_optimizer docs",
    )
    concentration_aversion: float = Field(
        default=1.0,
        ge=0,
        description="Weight (kappa) on concentration penalty in the objective",
    )


class OptimizationRequest(BaseModel):
    sources: list[YieldSourceInput] = Field(..., min_length=1)
    constraints: AllocationConstraints = Field(default_factory=AllocationConstraints)
    investable_amount_usd: float = Field(
        default=0.0,
        ge=0,
        description="Total capital to allocate; used to translate deposit caps into weight bounds",
    )


class AllocationWeight(BaseModel):
    """The optimizer's decision for one source."""

    source_id: str
    protocol: str
    weight: float = Field(ge=0, le=1, description="Allocation weight as a fraction of capital")
    weight_bps: int = Field(
        ge=0,
        le=10_000,
        description="Same weight in basis points (0-10000), for AllocationWeightEntry",
    )
    apy_pct: float
    risk_score: float
    eligible: bool = Field(
        description="Whether this source satisfied all hard constraints and was in the solve"
    )


class OptimizationResult(BaseModel):
    """Deterministic output of the optimizer. The LLM explanation layer may
    only restate numbers that appear in this model; it must never compute
    new ones."""

    feasible: bool
    weights: list[AllocationWeight] = Field(default_factory=list)
    expected_yield_pct: float = Field(
        default=0.0, description="Weighted-average expected APY of the allocation, in percent"
    )
    aggregate_risk_score: float = Field(
        default=0.0, description="Weighted-average risk score (0-100) of the allocation"
    )
    diversification_index: float = Field(
        default=0.0,
        description=(
            "1 - Herfindahl-Hirschman Index (HHI) of the weights, in [0, 1). "
            "0 means fully concentrated in one source; values closer to 1 mean more diversified."
        ),
    )
    objective_value: float = Field(
        default=0.0, description="Value of the risk-adjusted objective function at the solution"
    )
    method: str = Field(default="", description="Optimization method used")
    infeasibility_reasons: list[str] = Field(default_factory=list)


class AllocationExplanationResponse(BaseModel):
    """LLM-generated plain-language explanation of an `OptimizationResult`."""

    explanation: str
    grounded: bool = Field(
        description="Whether the explanation text introduced no numbers absent from the result"
    )


class OptimizationResponse(BaseModel):
    """Full API response: the deterministic result plus its explanation."""

    result: OptimizationResult
    explanation: AllocationExplanationResponse
