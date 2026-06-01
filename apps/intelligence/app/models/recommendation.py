from typing import Literal

from pydantic import BaseModel, Field

ConfidenceLevel = Literal["high", "medium", "low"]


class Recommendation(BaseModel):
    action: str
    rationale: str
    confidence: ConfidenceLevel
    confidence_reason: str = Field(..., alias="confidence_reason")
    data_freshness: str
    disclaimer: str


class RecommendedVault(BaseModel):
    vault_id: str
    allocation_pct: int = Field(ge=0, le=100)
    rationale: str


class VaultRecommendationRequest(BaseModel):
    risk_tolerance: Literal["conservative", "moderate", "aggressive"]
    time_horizon_months: int = Field(gt=0, le=120)
    initial_deposit_usdc: float = Field(gt=0)
    savings_goal: str | None = None


class VaultRecommendationResponse(BaseModel):
    recommended_vaults: list[RecommendedVault]
    expected_yield_usdc: float = Field(ge=0)
    confidence: ConfidenceLevel
