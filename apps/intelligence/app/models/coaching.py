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
    # User's preferred response language (ISO 639-1, e.g. "fr", "sw"). Shared
    # with the frontend i18n settings (#789); falls back to auto-detection
    # when unset (#multilingual).
    language: str | None = None
    # Opt-out (#935): callers that already know the user has disabled
    # AI-driven nudges/insights set this to False so the intelligence
    # service refuses to generate anything, as a second layer of
    # enforcement independent of the caller's own check. Defaults True so
    # existing/on-demand callers that don't set it (an explicit,
    # user-initiated "get my coaching" request, which opt-out doesn't
    # apply to) are unaffected.
    ai_insights_enabled: bool = True


class DepositScheduleItem(BaseModel):
    date: str
    amount_usdc: float
    note: str | None = None


class CoachingResponse(BaseModel):
    progress_assessment: str
    deposit_schedule: list[DepositScheduleItem]
    nudges: list[str]
    confidence: str = "medium"
