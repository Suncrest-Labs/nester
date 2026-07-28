"""Tests for the LLM explanation layer of the yield optimization engine (#848).

Required behavior under test: the explanation returned to the caller never
introduces a number absent from the computed `OptimizationResult` -- whether
the underlying LLM call succeeds, fails, or returns ungrounded text.

Follows the hand-rolled Anthropic mock pattern used in test_recommendation.py
(`FakeClient`/`DummyTextBlock`), rather than a real network call.
"""

from types import SimpleNamespace

import pytest

from app.models.optimization import AllocationWeight, OptimizationResult
from app.services import yield_explanation


class DummyTextBlock:
    def __init__(self, text: str) -> None:
        self.text = text


class FakeMessages:
    def __init__(self, payload: str) -> None:
        self.payload = payload

    async def create(self, *args: object, **kwargs: object) -> SimpleNamespace:
        return SimpleNamespace(content=[DummyTextBlock(self.payload)])


class FakeClient:
    def __init__(self, payload: str) -> None:
        self.messages = FakeMessages(payload)


class RaisingMessages:
    async def create(self, *args: object, **kwargs: object) -> SimpleNamespace:
        raise RuntimeError("simulated Anthropic API failure")


class RaisingClient:
    def __init__(self) -> None:
        self.messages = RaisingMessages()


def _feasible_result() -> OptimizationResult:
    return OptimizationResult(
        feasible=True,
        weights=[
            AllocationWeight(
                source_id="a",
                protocol="Aave",
                weight=0.6,
                weight_bps=6000,
                apy_pct=30.0,
                risk_score=5.0,
                eligible=True,
            ),
            AllocationWeight(
                source_id="b",
                protocol="Blend",
                weight=0.4,
                weight_bps=4000,
                apy_pct=5.0,
                risk_score=60.0,
                eligible=True,
            ),
        ],
        expected_yield_pct=20.0,
        aggregate_risk_score=27.0,
        diversification_index=0.48,
        objective_value=0.1,
        method="scipy.optimize.minimize(SLSQP) on a concave-quadratic risk-adjusted objective",
        infeasibility_reasons=[],
    )


def _infeasible_result() -> OptimizationResult:
    return OptimizationResult(
        feasible=False,
        weights=[
            AllocationWeight(
                source_id="a",
                protocol="Aave",
                weight=0.0,
                weight_bps=0,
                apy_pct=10.0,
                risk_score=20.0,
                eligible=False,
            ),
        ],
        expected_yield_pct=0.0,
        aggregate_risk_score=0.0,
        diversification_index=0.0,
        objective_value=0.0,
        method="scipy.optimize.minimize(SLSQP) on a concave-quadratic risk-adjusted objective",
        infeasibility_reasons=["Eligible sources can supply at most 0.00% liquidity, ..."],
    )


@pytest.mark.asyncio
async def test_grounded_explanation_is_returned_as_is(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(
        yield_explanation.anthropic.types, "TextBlock", DummyTextBlock, raising=False
    )
    result = _feasible_result()
    payload = (
        "This allocation puts 60% into Aave and 40% into Blend. Aave has an "
        "APY of 30.00% with a low risk score of 5, while Blend offers 5.00% "
        "APY at a higher risk score of 60. Overall the mix has an expected "
        "yield of 20.00% and an aggregate risk score of 27."
    )
    client = FakeClient(payload)

    explanation = await yield_explanation.explain_allocation(result, client=client)  # type: ignore[arg-type]

    assert explanation.grounded is True
    assert explanation.explanation == payload


@pytest.mark.asyncio
async def test_explanation_with_invented_number_is_replaced_and_flagged(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """This is the required, tested behavior: an explanation that introduces
    a number absent from the computed result must never reach the caller."""
    monkeypatch.setattr(
        yield_explanation.anthropic.types, "TextBlock", DummyTextBlock, raising=False
    )
    result = _feasible_result()
    payload = (
        "This allocation puts 60% into Aave and 40% into Blend, projected to "
        "grow to $9,999 by next year."  # 9999 is nowhere in the computed result
    )
    client = FakeClient(payload)

    explanation = await yield_explanation.explain_allocation(result, client=client)  # type: ignore[arg-type]

    assert explanation.grounded is False
    assert "9999" not in explanation.explanation
    assert "9,999" not in explanation.explanation
    # The fallback explanation must still convey the real computed numbers.
    assert "20.00%" in explanation.explanation or "20" in explanation.explanation


@pytest.mark.asyncio
async def test_explanation_numbers_all_traceable_to_result_for_many_phrasings(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Sweep a few plausible ways the model might phrase the same numbers and
    confirm each is accepted as grounded (no false positives from rounding)."""
    monkeypatch.setattr(
        yield_explanation.anthropic.types, "TextBlock", DummyTextBlock, raising=False
    )
    result = _feasible_result()
    grounded_payloads = [
        "60.0% goes to Aave (6000 bps) and 40.0% to Blend (4000 bps).",
        "About 60% in Aave, about 40% in Blend. Expected yield: 20%.",
        "Aave: 30% APY / risk 5. Blend: 5% APY / risk 60. Aggregate risk: 27.",
    ]
    for payload in grounded_payloads:
        client = FakeClient(payload)
        explanation = await yield_explanation.explain_allocation(result, client=client)  # type: ignore[arg-type]
        assert explanation.grounded is True, f"unexpectedly flagged: {payload!r}"
        assert explanation.explanation == payload


@pytest.mark.asyncio
async def test_llm_failure_falls_back_to_grounded_deterministic_explanation() -> None:
    result = _feasible_result()
    client = RaisingClient()

    explanation = await yield_explanation.explain_allocation(result, client=client)  # type: ignore[arg-type]

    assert explanation.grounded is False
    assert "Aave" in explanation.explanation
    assert "Blend" in explanation.explanation


@pytest.mark.asyncio
async def test_infeasible_result_never_calls_the_llm_and_reports_reasons() -> None:
    result = _infeasible_result()

    # Client is a RaisingClient to prove the LLM is never invoked for an
    # infeasible result -- the explanation is built purely from server-side
    # infeasibility_reasons.
    client = RaisingClient()

    explanation = await yield_explanation.explain_allocation(result, client=client)  # type: ignore[arg-type]

    assert explanation.grounded is True
    assert "liquidity" in explanation.explanation.lower()
