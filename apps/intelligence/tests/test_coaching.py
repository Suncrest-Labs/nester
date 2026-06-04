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
