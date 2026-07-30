"""Tests for the LLM narration layer of predictive protocol-health
deterioration assessments (#857).

Required behavior under test: the summary returned to the caller never
introduces a number absent from the computed `DeteriorationAssessment` --
whether the underlying LLM call succeeds, fails, or returns ungrounded text.

Follows the hand-rolled Anthropic mock pattern used in
test_yield_explanation.py (`FakeClient`/`DummyTextBlock`), rather than a real
network call.
"""

from types import SimpleNamespace

import pytest

from app.models.deterioration import DeteriorationAssessment
from app.services import deterioration_summary


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


def _deteriorating_assessment() -> DeteriorationAssessment:
    return DeteriorationAssessment(
        protocol_slug="aave",
        probability=0.82,
        level="severe",
        tvl_outflow_velocity_pct=42.0,
        apy_abnormality_z_score=3.1,
        reported_vs_derived_gap_pct=18.0,
        price_instability=0.22,
    )


@pytest.mark.asyncio
async def test_grounded_summary_is_returned_as_is(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(
        deterioration_summary.anthropic.types, "TextBlock", DummyTextBlock, raising=False
    )
    assessment = _deteriorating_assessment()
    payload = (
        "aave is showing severe deterioration risk at 82% probability. TVL has fallen "
        "42% and APY has spiked sharply (z-score 3.1), with an 18% gap between reported "
        "and derived APY."
    )
    client = FakeClient(payload)

    summary = await deterioration_summary.summarize_assessment(assessment, client=client)  # type: ignore[arg-type]

    assert summary.grounded is True
    assert summary.summary == payload


@pytest.mark.asyncio
async def test_summary_with_invented_number_is_replaced_and_flagged(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """This is the required, tested behavior: a summary that introduces a
    number absent from the computed assessment must never reach the caller."""
    monkeypatch.setattr(
        deterioration_summary.anthropic.types, "TextBlock", DummyTextBlock, raising=False
    )
    assessment = _deteriorating_assessment()
    payload = (
        "aave is at severe risk and could lose $9,999,999 in the next week."
    )  # 9999999 is nowhere in the computed assessment
    client = FakeClient(payload)

    summary = await deterioration_summary.summarize_assessment(assessment, client=client)  # type: ignore[arg-type]

    assert summary.grounded is False
    assert "9999999" not in summary.summary
    assert "9,999,999" not in summary.summary
    # The fallback summary must still convey the real computed numbers.
    assert "82%" in summary.summary


@pytest.mark.asyncio
async def test_llm_failure_falls_back_to_grounded_deterministic_summary() -> None:
    assessment = _deteriorating_assessment()
    client = RaisingClient()

    summary = await deterioration_summary.summarize_assessment(assessment, client=client)  # type: ignore[arg-type]

    assert summary.grounded is False
    assert "aave" in summary.summary


@pytest.mark.asyncio
async def test_user_audience_produces_reassuring_fund_move_explanation() -> None:
    assessment = _deteriorating_assessment()
    # Force the fallback path, which is deterministic and easy to assert on.
    client = RaisingClient()

    summary = await deterioration_summary.summarize_assessment(
        assessment, audience="user", client=client  # type: ignore[arg-type]
    )

    assert summary.grounded is False
    assert "protective" in summary.summary.lower()
    assert "aave" in summary.summary


@pytest.mark.asyncio
async def test_healthy_protocol_summary_mentions_no_dominant_driver() -> None:
    assessment = DeteriorationAssessment(
        protocol_slug="compound",
        probability=0.05,
        level="none",
        tvl_outflow_velocity_pct=0.0,
        apy_abnormality_z_score=0.1,
        reported_vs_derived_gap_pct=1.0,
        price_instability=0.01,
    )
    client = RaisingClient()

    summary = await deterioration_summary.summarize_assessment(assessment, client=client)  # type: ignore[arg-type]

    assert summary.grounded is False
    assert "compound" in summary.summary
    assert "no single dominant signal" in summary.summary
