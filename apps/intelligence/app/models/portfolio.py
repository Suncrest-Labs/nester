"""Portfolio analysis models for structured Claude tool use responses."""

from datetime import datetime
from typing import Literal

from pydantic import BaseModel, Field

ConfidenceLevel = Literal["high", "medium", "low"]
RiskLevel = Literal["conservative", "moderate", "aggressive"]


class AllocationItem(BaseModel):
    """Individual protocol allocation in the portfolio breakdown."""

    protocol: str = Field(..., description="Protocol name (e.g., Aave, Blend)")
    weight: float = Field(
        ..., ge=0, le=100, description="Allocation weight as percentage"
    )
    apy: float = Field(..., ge=0, description="Current APY for this protocol")


class PortfolioBreakdown(BaseModel):
    """Structured portfolio analysis produced by Claude tool use."""

    total_value_usdc: float = Field(
        ..., ge=0, description="Total portfolio value in USDC"
    )
    yield_30d_usdc: float = Field(
        ..., ge=0, description="Expected yield over 30 days in USDC"
    )
    allocation_breakdown: list[AllocationItem] = Field(
        ..., description="Breakdown by protocol with weights and APYs"
    )
    risk_level: RiskLevel = Field(..., description="Overall portfolio risk assessment")
    top_recommendation: str = Field(
        ..., description="Primary recommendation for the user"
    )
    rebalance_suggested: bool = Field(
        ..., description="Whether rebalancing is recommended"
    )


class PortfolioAnalysisResponse(BaseModel):
    """Complete response from portfolio analysis endpoint."""

    analysis: PortfolioBreakdown = Field(
        ..., description="Structured portfolio breakdown"
    )
    narrative: str = Field(..., description="Explanation narrative from Claude")
    confidence: ConfidenceLevel = Field(
        ..., description="Confidence level of the analysis"
    )
    generated_at: datetime = Field(
        default_factory=lambda: datetime.now(),
        description="ISO timestamp when analysis was generated",
    )
