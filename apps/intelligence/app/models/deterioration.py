"""Data models for the predictive protocol-health deterioration summary
endpoint (#857).

The deterioration score/probability/level and the list of driving indicators
are all computed deterministically on the Go side
(`internal/scheduler/deterioration_score.go`) and passed in verbatim as
`DeteriorationAssessment`. This service's only job is to have Prometheus
narrate an already-computed assessment in plain language for an operator or,
for a user-facing message, explain why their funds were moved -- exactly the
same "explain, never compute" split `yield_explanation.py` already
established for the optimizer.
"""

import re
from typing import Literal

from pydantic import BaseModel, Field

DeteriorationLevel = Literal["none", "mild", "moderate", "severe"]

# Matches the lowercase-slug shape every protocol identifier already uses
# elsewhere in this codebase (e.g. "aave", "blend"), plus room for hyphenated
# multi-word names. protocol_slug is interpolated directly into the
# Anthropic prompt in deterioration_summary.py — a valid-JWT caller with an
# unbounded/arbitrary value here could otherwise inject prompt directives or
# inflate prompt token usage.
_PROTOCOL_SLUG_RE = re.compile(r"^[a-z0-9][a-z0-9-]{0,63}$")


class DeteriorationAssessment(BaseModel):
    """A single scored protocol-health assessment, computed on the Go side."""

    protocol_slug: str = Field(pattern=_PROTOCOL_SLUG_RE.pattern, max_length=64)
    probability: float = Field(ge=0.0, le=1.0)
    level: DeteriorationLevel
    tvl_outflow_velocity_pct: float
    apy_abnormality_z_score: float
    reported_vs_derived_gap_pct: float
    price_instability: float


class DeteriorationSummaryRequest(BaseModel):
    assessment: DeteriorationAssessment
    # audience selects the tone/target of the narration: "operator" for an
    # internal dashboard summary, "user" for the "why we moved your funds"
    # message shown to an affected depositor.
    audience: Literal["operator", "user"] = "operator"


class DeteriorationSummaryResponse(BaseModel):
    """LLM-generated plain-language narration of a `DeteriorationAssessment`."""

    summary: str
    grounded: bool = Field(
        description="Whether the summary text introduced no numbers absent from the assessment"
    )
