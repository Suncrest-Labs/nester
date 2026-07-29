from types import SimpleNamespace

import pytest

from app.models.coaching import CoachingRequest, PortfolioContext, SavingsGoalContext
from app.services import prometheus


class DummyTextBlock:
    def __init__(self, text: str) -> None:
        self.text = text


class FakeMessages:
    def __init__(self, payload: str) -> None:
        self.payload = payload

    async def create(self, *args, **kwargs):
        return SimpleNamespace(content=[DummyTextBlock(self.payload)])


class FakeClient:
    def __init__(self, payload: str) -> None:
        self.messages = FakeMessages(payload)


@pytest.mark.asyncio
async def test_generate_coaching_returns_structured_schedule(monkeypatch):
    payload = (
        '{"progress_assessment": "You are on track.", '
        '"deposit_schedule": ['
        '{"date": "2026-06-15", "amount_usdc": 100, "note": "First deposit"}], '
        '"nudges": ["Great start!"], "confidence": "high"}'
    )
    monkeypatch.setattr(prometheus, "get_client", lambda: FakeClient(payload))
    monkeypatch.setattr(prometheus.anthropic.types, "TextBlock", DummyTextBlock, raising=False)

    result = await prometheus.generate_coaching(
        CoachingRequest(
            goal=SavingsGoalContext(
                target_amount=1000,
                currency="USDC",
                deadline="2026-12-31T00:00:00Z",
                current_amount=200,
                progress_pct=20,
            ),
            portfolio=PortfolioContext(total_balance_usd=500),
        )
    )

    assert result.confidence == "high"
    assert len(result.deposit_schedule) == 1
    assert result.deposit_schedule[0].amount_usdc == 100
    assert "on track" in result.progress_assessment


@pytest.mark.asyncio
async def test_generate_coaching_respects_opt_out_flag(monkeypatch):
    """#935: ai_insights_enabled=False must refuse generation without ever
    calling Claude — the intelligence service's own enforcement layer,
    independent of whether the caller (the Go API) already checked."""

    def _unexpected_call() -> FakeClient:
        raise AssertionError("get_client() should not be called when opted out")

    monkeypatch.setattr(prometheus, "get_client", _unexpected_call)

    result = await prometheus.generate_coaching(
        CoachingRequest(
            goal=SavingsGoalContext(
                target_amount=1000,
                currency="USDC",
                deadline="2026-12-31T00:00:00Z",
                current_amount=200,
                progress_pct=20,
            ),
            portfolio=PortfolioContext(total_balance_usd=500),
            ai_insights_enabled=False,
        )
    )

    assert result.progress_assessment == ""
    assert result.deposit_schedule == []
    assert result.nudges == []


@pytest.mark.asyncio
async def test_generate_coaching_defaults_to_enabled_when_flag_omitted(monkeypatch):
    """Existing/on-demand callers that don't set ai_insights_enabled at all
    (an explicit, user-initiated request — opt-out doesn't apply) must be
    unaffected by the new field."""
    payload = (
        '{"progress_assessment": "You are on track.", '
        '"deposit_schedule": [], "nudges": [], "confidence": "high"}'
    )
    monkeypatch.setattr(prometheus, "get_client", lambda: FakeClient(payload))
    monkeypatch.setattr(prometheus.anthropic.types, "TextBlock", DummyTextBlock, raising=False)

    request = CoachingRequest(
        goal=SavingsGoalContext(
            target_amount=1000,
            currency="USDC",
            deadline="2026-12-31T00:00:00Z",
        ),
        portfolio=PortfolioContext(),
    )
    assert request.ai_insights_enabled is True

    result = await prometheus.generate_coaching(request)
    assert "on track" in result.progress_assessment
