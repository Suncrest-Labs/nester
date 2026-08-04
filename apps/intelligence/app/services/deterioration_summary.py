"""LLM narration layer for predictive protocol-health deterioration
assessments (#857).

The deterioration probability, level, and every indicator value are computed
deterministically on the Go side
(`internal/scheduler/deterioration_score.go`'s `Score`/`ComputeIndicators`).
This module's only job is to have Claude narrate an already-scored
`DeteriorationAssessment` in plain language -- for an operator dashboard, or
for the "why we moved your funds" message shown to an affected user. It never
computes, adjusts, or re-derives the score or any indicator itself.

Grounding follows the exact same pattern `yield_explanation.py` established
for the optimizer: every number the model is allowed to mention is collected
from the assessment's own fields first; the model's response is then scanned
for numeric tokens and, if it introduces any number outside that set, it is
discarded in favor of a deterministic, template-built summary that by
construction cannot contain an unsupported number.
"""

from __future__ import annotations

import logging
from typing import Optional

import anthropic

from app.config import settings
from app.models.deterioration import DeteriorationAssessment, DeteriorationSummaryResponse
from app.services.retrieval import extract_numbers, normalize_number

logger = logging.getLogger(__name__)

SUMMARY_MAX_TOKENS = 300

_OPERATOR_SYSTEM_PROMPT = """You are Prometheus, summarizing a protocol-health deterioration \
assessment for a Nester operator.

The assessment below was already computed by a deterministic scoring model. You are explaining \
the result, not producing it.

STRICT rules:
1. Use ONLY the numbers given to you in the "Computed assessment" block. Do not invent, \
   estimate, or project any number not already present there.
2. You may restate a given number rounded for readability, but it must always correspond to a \
   number in the computed assessment.
3. Write 2-4 plain-language sentences an operator can act on in seconds: what is happening, how \
   confident the model is, and which indicators are driving it.
4. Never use em dashes. Keep it direct and free of jargon.
"""

_USER_SYSTEM_PROMPT = """You are Prometheus, explaining to a Nester user why their funds were \
moved out of a yield source.

The assessment below was already computed by a deterministic scoring model, and the fund move \
already happened (or is being recommended) as a result. You are explaining why, not deciding \
whether to do it.

STRICT rules:
1. Use ONLY the numbers given to you in the "Computed assessment" block. Do not invent, \
   estimate, or project any number not already present there.
2. You may restate a given number rounded for readability, but it must always correspond to a \
   number in the computed assessment.
3. Write 2-3 reassuring, plain-language sentences: what changed, why it mattered for their \
   funds, and that this was a protective, deliberate action, not an error.
4. Never use em dashes. Keep it direct and free of jargon.
"""

_client: Optional[anthropic.AsyncAnthropic] = None


def get_client() -> anthropic.AsyncAnthropic:
    global _client
    if _client is None:
        _client = anthropic.AsyncAnthropic(api_key=settings.anthropic_api_key)
    return _client


def _allowed_numbers(assessment: DeteriorationAssessment) -> set[str]:
    """Every number that legitimately describes `assessment`, at a few
    common roundings, normalized the same way `extract_numbers` normalizes
    prose."""
    values: list[float] = [
        assessment.probability,
        assessment.probability * 100.0,
        assessment.tvl_outflow_velocity_pct,
        assessment.apy_abnormality_z_score,
        assessment.reported_vs_derived_gap_pct,
        assessment.price_instability,
        assessment.price_instability * 100.0,
    ]
    numbers: set[str] = set()
    for v in values:
        for decimals in (0, 1, 2):
            numbers.add(normalize_number(f"{v:.{decimals}f}"))
    return numbers


def _fallback_summary(assessment: DeteriorationAssessment, audience: str) -> str:
    """Deterministic, template-built summary using only fields already
    present in `assessment`. Used when the model is unavailable or its own
    text fails the grounding check, so a safe summary is always returned."""
    drivers: list[str] = []
    if assessment.tvl_outflow_velocity_pct > 5:
        drivers.append(f"TVL down {assessment.tvl_outflow_velocity_pct:.0f}% in the window")
    if abs(assessment.apy_abnormality_z_score) > 1:
        direction = "spiked" if assessment.apy_abnormality_z_score > 0 else "collapsed"
        drivers.append(f"APY {direction} (z-score {assessment.apy_abnormality_z_score:.1f})")
    if assessment.reported_vs_derived_gap_pct > 5:
        drivers.append(
            f"a {assessment.reported_vs_derived_gap_pct:.0f}% reported-vs-derived APY gap"
        )
    driver_text = "; ".join(drivers) if drivers else "no single dominant signal"

    if audience == "user":
        return (
            f"We reduced your exposure to {assessment.protocol_slug} as a protective measure. "
            f"Our monitoring flagged {driver_text}, putting the estimated deterioration "
            f"probability at {assessment.probability * 100:.0f}%. This was a deliberate, "
            "bounded action to protect your funds, not an error."
        )
    return (
        f"{assessment.protocol_slug}: {assessment.level} deterioration risk at "
        f"{assessment.probability * 100:.0f}% probability. Driven by {driver_text}."
    )


def _build_prompt(assessment: DeteriorationAssessment) -> str:
    return (
        "Computed assessment (the ONLY source of numbers you may use):\n"
        f"- Protocol: {assessment.protocol_slug}\n"
        f"- Deterioration level: {assessment.level}\n"
        f"- Deterioration probability: {assessment.probability * 100:.2f}%\n"
        f"- TVL outflow velocity: {assessment.tvl_outflow_velocity_pct:.2f}%\n"
        f"- APY abnormality z-score: {assessment.apy_abnormality_z_score:.2f}\n"
        f"- Reported-vs-derived APY gap: {assessment.reported_vs_derived_gap_pct:.2f}%\n"
        "- TVL price instability (coefficient of variation): "
        f"{assessment.price_instability:.2f}\n\n"
        "Summarize this assessment."
    )


async def summarize_assessment(
    assessment: DeteriorationAssessment,
    audience: str = "operator",
    client: anthropic.AsyncAnthropic | None = None,
) -> DeteriorationSummaryResponse:
    """Produce a plain-language summary of an already-computed
    `DeteriorationAssessment`. Always returns a grounded summary: if the
    model's own text introduces a number not traceable to `assessment`, it
    is replaced by a deterministic fallback built from the assessment's own
    fields.
    """
    allowed = _allowed_numbers(assessment)
    prompt = _build_prompt(assessment)
    system_prompt = _USER_SYSTEM_PROMPT if audience == "user" else _OPERATOR_SYSTEM_PROMPT

    active_client = client or get_client()
    try:
        response = await active_client.messages.create(
            model=settings.anthropic_model,
            max_tokens=SUMMARY_MAX_TOKENS,
            system=system_prompt,
            messages=[{"role": "user", "content": prompt}],
        )
        text = next(
            (b.text for b in response.content if isinstance(b, anthropic.types.TextBlock)),
            "",
        ).strip()
    except Exception:
        logger.exception("deterioration assessment summary generation failed")
        return DeteriorationSummaryResponse(
            summary=_fallback_summary(assessment, audience), grounded=False
        )

    if not text:
        return DeteriorationSummaryResponse(
            summary=_fallback_summary(assessment, audience), grounded=False
        )

    unsupported = sorted(extract_numbers(text) - allowed)
    if unsupported:
        logger.warning(
            "deterioration assessment summary introduced unsupported numbers: %s",
            unsupported,
        )
        return DeteriorationSummaryResponse(
            summary=_fallback_summary(assessment, audience), grounded=False
        )

    return DeteriorationSummaryResponse(summary=text, grounded=True)
