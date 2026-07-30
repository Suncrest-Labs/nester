"""LLM explanation layer for the yield optimization engine (#848).

The optimizer (`app.services.yield_optimizer.optimize`) is the sole source of
the allocation itself: weights, expected yield, aggregate risk, and
diversification are all computed deterministically with no LLM involved. This
module's only job is to have Claude *narrate* an already-computed
`OptimizationResult` in plain language. It never asks the model to compute,
adjust, or re-derive anything.

## Grounding / no-hallucinated-numbers guarantee

Because the explanation must never introduce a number absent from the
computed result (a required, tested behavior), every model response is
validated before it is returned:

1. `_allowed_numbers()` collects every number that legitimately describes the
   result -- each weight as a percentage and in basis points, each source's
   APY and risk score, and the aggregate figures (expected yield, aggregate
   risk, diversification index, source counts) -- each rendered at 0, 1, and
   2 decimal places. Multiple roundings are allowed because prose naturally
   rounds figures ("about 60%" for 0.5988), but every allowed number is still
   directly traceable to a field of the computed result.
2. The model's raw text is scanned for numeric tokens with
   `app.services.retrieval.extract_numbers` (the same helper the chat
   grounding layer in `grounding.py` uses for #852), normalized the same way,
   and compared against the allowed set.
3. If the model's text contains any number outside that set, it is discarded
   and replaced with a deterministic, template-built explanation constructed
   directly from the result's own fields -- which by construction cannot
   contain an unsupported number. The `grounded` flag on the response tells
   the caller (and tests) whether the model's own text was usable (True) or
   had to be replaced by the safe fallback (False).

This mirrors the "your call on exact validation strategy, document it"
guidance in the issue: rather than constraining the model to enum/reference
fields only, we let it write free prose but verify every digit sequence in it
post hoc, exactly as `grounding.py` already does for chat answers.
"""

from __future__ import annotations

import logging
from typing import Optional

import anthropic

from app.config import settings
from app.models.optimization import AllocationExplanationResponse, OptimizationResult
from app.services.retrieval import extract_numbers, normalize_number

logger = logging.getLogger(__name__)

EXPLAIN_MAX_TOKENS = 400

_SYSTEM_PROMPT = """You are Prometheus, narrating a yield allocation for a Nester user.

The allocation below was already computed by a deterministic constrained optimizer. You are
explaining the result, not producing it.

STRICT rules:
1. Use ONLY the numbers given to you in the "Computed result" block. Do not invent, estimate,
   or project any number (no projected dollar amounts, no new percentages, no dates) that is
   not already present there.
2. You may restate a given number rounded for readability (e.g. "about 60%" for 59.9%), but it
   must always correspond to a number in the computed result.
3. Write 3-5 plain-language sentences explaining what was allocated and why it serves a
   risk-adjusted-yield goal under the stated constraints (diversification, liquidity, risk
   ceiling, lock horizon).
4. Never use em dashes. Keep it direct and free of jargon.
"""

_client: Optional[anthropic.AsyncAnthropic] = None


def get_client() -> anthropic.AsyncAnthropic:
    global _client
    if _client is None:
        _client = anthropic.AsyncAnthropic(api_key=settings.anthropic_api_key)
    return _client


def _allowed_numbers(result: OptimizationResult) -> set[str]:
    """Every number that legitimately describes `result`, at a few common
    roundings, normalized the same way `extract_numbers` normalizes prose."""
    values: list[float] = [
        result.expected_yield_pct,
        result.aggregate_risk_score,
        result.diversification_index,
        result.diversification_index * 100.0,
        float(len(result.weights)),
        float(sum(1 for w in result.weights if w.eligible)),
    ]
    for w in result.weights:
        values.extend(
            [
                w.weight * 100.0,
                float(w.weight_bps),
                w.apy_pct,
                w.risk_score,
            ]
        )

    numbers: set[str] = set()
    for v in values:
        for decimals in (0, 1, 2):
            numbers.add(normalize_number(f"{v:.{decimals}f}"))
    return numbers


def _fallback_infeasible_explanation(result: OptimizationResult) -> str:
    reasons = "; ".join(result.infeasibility_reasons) or (
        "the constraints could not all be satisfied at the same time"
    )
    return (
        "No allocation can satisfy all of your constraints at once, so none is being "
        f"recommended. {reasons} Consider relaxing one limit, such as the risk ceiling, "
        "the liquidity floor, the diversification cap, or the lock horizon, and trying again."
    )


def _fallback_explanation(result: OptimizationResult) -> str:
    """Deterministic, template-built explanation using only fields already
    present in `result`. Used when the model is unavailable or its own text
    fails the grounding check, so a safe explanation is always returned."""
    if not result.feasible:
        return _fallback_infeasible_explanation(result)

    allocated = [w for w in result.weights if w.weight_bps > 0]
    if not allocated:
        return "No capital could be allocated under the current constraints."

    parts = [
        f"{w.protocol} at {w.weight * 100:.1f}% (APY {w.apy_pct:.2f}%, risk score "
        f"{w.risk_score:.0f}/100)"
        for w in allocated
    ]
    return (
        "Recommended allocation: " + "; ".join(parts) + ". "
        f"This mix has an expected yield of {result.expected_yield_pct:.2f}% and an "
        f"aggregate risk score of {result.aggregate_risk_score:.0f}/100, with a "
        f"diversification index of {result.diversification_index:.2f} "
        "(1.0 is fully spread out, 0 is concentrated in a single source)."
    )


def _build_prompt(result: OptimizationResult) -> str:
    allocated = [w for w in result.weights if w.weight_bps > 0]
    lines = [
        f"- {w.protocol}: {w.weight * 100:.2f}% ({w.weight_bps} bps), "
        f"APY {w.apy_pct:.2f}%, risk score {w.risk_score:.0f}/100"
        for w in allocated
    ]
    return (
        "Computed result (the ONLY source of numbers you may use):\n"
        f"- Expected portfolio yield: {result.expected_yield_pct:.2f}%\n"
        f"- Aggregate risk score: {result.aggregate_risk_score:.0f}/100\n"
        f"- Diversification index: {result.diversification_index:.2f} "
        "(0-1, higher is more spread out)\n"
        f"- Sources allocated to ({len(allocated)}):\n" + "\n".join(lines) + "\n\n"
        "Explain this allocation to the user."
    )


async def explain_allocation(
    result: OptimizationResult,
    client: anthropic.AsyncAnthropic | None = None,
) -> AllocationExplanationResponse:
    """Produce a plain-language explanation of an already-computed
    `OptimizationResult`. Always returns a grounded explanation: if the
    model's own text introduces a number not traceable to `result`, it is
    replaced by a deterministic fallback built from `result`'s own fields.
    """
    if not result.feasible:
        # Infeasibility is reported verbatim from server-computed reasons;
        # no LLM call is needed or made.
        return AllocationExplanationResponse(
            explanation=_fallback_infeasible_explanation(result), grounded=True
        )

    allowed = _allowed_numbers(result)
    prompt = _build_prompt(result)

    active_client = client or get_client()
    try:
        response = await active_client.messages.create(
            model=settings.anthropic_model,
            max_tokens=EXPLAIN_MAX_TOKENS,
            system=_SYSTEM_PROMPT,
            messages=[{"role": "user", "content": prompt}],
        )
        text = next(
            (b.text for b in response.content if isinstance(b, anthropic.types.TextBlock)),
            "",
        ).strip()
    except Exception:
        logger.exception("yield optimization explanation generation failed")
        return AllocationExplanationResponse(
            explanation=_fallback_explanation(result), grounded=False
        )

    if not text:
        return AllocationExplanationResponse(
            explanation=_fallback_explanation(result), grounded=False
        )

    unsupported = sorted(extract_numbers(text) - allowed)
    if unsupported:
        logger.warning(
            "yield optimization explanation introduced unsupported numbers: %s",
            unsupported,
        )
        return AllocationExplanationResponse(
            explanation=_fallback_explanation(result), grounded=False
        )

    return AllocationExplanationResponse(explanation=text, grounded=True)
