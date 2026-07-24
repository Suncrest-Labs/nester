"""Pydantic models for savings coaching."""

from typing import Any

from pydantic import BaseModel, Field

from app.services.guardrails import MAX_USER_MESSAGE_CHARS


class SavingsGoalContext(BaseModel):
    id: str | None = None
    target_amount: float
    currency: str = "USDC"
    deadline: str
    description: str | None = Field(default=None, max_length=MAX_USER_MESSAGE_CHARS)
    current_amount: float = 0
    progress_pct: float = 0


class PortfolioContext(BaseModel):
    total_balance_usd: float = 0
    vaults: list[dict[str, Any]] = Field(default_factory=list)


class CoachingRequest(BaseModel):
    goal: SavingsGoalContext
    portfolio: PortfolioContext


class DepositScheduleItem(BaseModel):
    date: str
    amount_usdc: float
    note: str | None = None


class CoachingResponse(BaseModel):
    progress_assessment: str
    deposit_schedule: list[DepositScheduleItem]
    nudges: list[str]
    confidence: str = "medium"
