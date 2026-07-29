"""Models for the personalized savings recommendation engine (#847).

Architecture: every number in a `RecommendationCandidate` is computed
deterministically in Python (see `app.services.recommendation_engine`) from
real, fetched user/platform data -- goal targets/deadlines, vault balances and
APYs, and (where available) the Go API's own computed outputs (e.g. the
vault rebalance service). The LLM is only ever shown these candidates and
their pre-computed impacts; its role is to select, prioritize, and explain
them in plain language. It never computes a financial figure itself, and a
fabrication guard (`app.services.recommendation_engine._validate_selection`)
rejects/regenerates any LLM output that states a number not present in the
candidate it claims to explain.
"""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Literal, Optional

from pydantic import BaseModel, Field

CandidateActionType = Literal[
    "increase_contribution",
    "move_to_higher_yield",
    "lock_for_term_boost",
    "consolidate_goals",
]

# Where a candidate's probabilistic/yield figures were computed:
# - "monte_carlo": the (future) Monte Carlo projection service (#843)
# - "rebalance_service": the Go API's already-computed vault rebalance suggestion
# - "heuristic": simpler deterministic math computed locally when neither of
#   the above is available -- still deterministic, never LLM-estimated.
ProjectionSource = Literal["monte_carlo", "heuristic", "rebalance_service"]

STANDARD_DISCLAIMER = (
    "Nester is non-custodial: you always control your funds. This is educational "
    "guidance, not financial advice, and every projection is a probabilistic "
    "estimate, not a guarantee of future results."
)


class RecommendationImpact(BaseModel):
    """Computed impact of taking a candidate action. Every field here is the
    direct output of deterministic Python math (see recommendation_engine.py)
    or a value passed through unchanged from a Go API computation -- never an
    LLM estimate."""

    goal_success_probability_delta: Optional[float] = Field(
        default=None,
        description=(
            "Change in probability of hitting the goal by its deadline, "
            "roughly in the range [-1, 1]. Populated from the Monte Carlo "
            "projection service when available, else a documented heuristic."
        ),
    )
    additional_yield_usdc: Optional[float] = Field(
        default=None, description="Additional USDC yield expected over the horizon."
    )
    time_saved_months: Optional[float] = Field(
        default=None, description="Months saved reaching the goal deadline."
    )
    projection_source: ProjectionSource = Field(
        default="heuristic",
        description="Where the probability/yield figures were computed.",
    )


class RecommendationCandidate(BaseModel):
    """A deterministically-generated candidate action. This is the ONLY place
    numbers enter the recommendation pipeline -- the LLM downstream never adds
    a new figure, it only references `candidate_id` and writes prose."""

    candidate_id: str
    action_type: CandidateActionType
    title: str
    summary: str = Field(
        description=(
            "Deterministic, factual one-line summary containing every number "
            "relevant to this candidate. Used both to prompt the LLM and to "
            "validate its prose against (the fabrication guard)."
        )
    )
    target_id: Optional[str] = Field(
        default=None, description="The goal_id or vault_id this candidate acts on."
    )
    impact: RecommendationImpact
    risk_context: Optional[str] = Field(
        default=None,
        description="Required (non-null) for any yield-related candidate type.",
    )
    priority_score: float = Field(
        default=0.0,
        description=(
            "Deterministic ranking score (impact magnitude + engagement "
            "weighting) used to pre-order candidates before the LLM sees them."
        ),
    )


class SavingsRecommendationItem(BaseModel):
    """One LLM-selected, explained recommendation shown to the user."""

    candidate_id: str
    action_type: CandidateActionType
    title: str
    explanation: str
    impact: RecommendationImpact
    risk_context: Optional[str] = None
    priority: int = Field(ge=1)


class SavingsRecommendationSet(BaseModel):
    """The full response returned to the client."""

    user_id: str
    recommendations: list[SavingsRecommendationItem]
    disclaimer: str = STANDARD_DISCLAIMER
    data_freshness: str = ""
    generated_at: datetime = Field(default_factory=lambda: datetime.now(timezone.utc))
