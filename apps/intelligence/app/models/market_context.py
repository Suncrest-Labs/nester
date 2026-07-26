"""Structured, low-trust market context models."""

from datetime import datetime, timezone
from enum import Enum

from pydantic import BaseModel, Field, HttpUrl, field_validator


class SignalType(str, Enum):
    ANNOUNCEMENT = "announcement"
    SECURITY_CONCERN = "security_concern"
    SENTIMENT_SHIFT = "sentiment_shift"
    DEPEG_RISK = "depeg_risk"


class SignalDirection(str, Enum):
    POSITIVE = "positive"
    NEGATIVE = "negative"
    NEUTRAL = "neutral"


class SourceDocument(BaseModel):
    protocol: str = Field(min_length=1, max_length=80)
    asset: str | None = Field(default=None, max_length=32)
    source_url: HttpUrl
    publisher: str = Field(min_length=1, max_length=120)
    content: str = Field(min_length=1, max_length=20_000)
    published_at: datetime


class ExtractedSignal(BaseModel):
    protocol: str = Field(min_length=1, max_length=80)
    asset: str | None = Field(default=None, max_length=32)
    signal_type: SignalType
    direction: SignalDirection
    confidence: float = Field(ge=0, le=1)
    summary: str = Field(min_length=1, max_length=500)
    source_url: HttpUrl
    publisher: str = Field(min_length=1, max_length=120)
    observed_at: datetime = Field(default_factory=lambda: datetime.now(timezone.utc))
    corroborating_sources: list[HttpUrl] = Field(default_factory=list)
    advisory_only: bool = True

    @field_validator("advisory_only")
    @classmethod
    def require_advisory_boundary(cls, value: bool) -> bool:
        if not value:
            raise ValueError("market-context signals must remain advisory")
        return value


class MarketContextResponse(BaseModel):
    signal: str
    summary: str
    confidence: float = Field(ge=0, le=1)
    updatedAt: datetime
    contexts: list[ExtractedSignal]
    disclaimer: str = (
        "Market context is low-trust information, not financial advice. "
        "It cannot trigger fund movements."
    )
