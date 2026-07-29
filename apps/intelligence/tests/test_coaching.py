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
        self.last_kwargs: dict | None = None

    async def create(self, *args, **kwargs):
        self.last_kwargs = kwargs
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
async def test_generate_coaching_localizes_system_prompt_for_preferred_language(
    monkeypatch,
):
    """#multilingual: the coaching system prompt is localized to the goal's
    stored language preference, using the established local financial term
    rather than a literal translation.
    """
    payload = (
        '{"progress_assessment": "En bonne voie.", '
        '"deposit_schedule": [], "nudges": [], "confidence": "high"}'
    )
    fake_client = FakeClient(payload)
    monkeypatch.setattr(prometheus, "get_client", lambda: fake_client)
    monkeypatch.setattr(prometheus.anthropic.types, "TextBlock", DummyTextBlock, raising=False)

    await prometheus.generate_coaching(
        CoachingRequest(
            goal=SavingsGoalContext(
                target_amount=1000,
                currency="USDC",
                deadline="2026-12-31T00:00:00Z",
                current_amount=200,
                progress_pct=20,
            ),
            portfolio=PortfolioContext(total_balance_usd=500),
            language="fr",
        )
    )

    sent_system = fake_client.messages.last_kwargs["system"]
    assert "Respond natively in French" in sent_system
    assert "intérêts composés" in sent_system  # established term, not translated


@pytest.mark.asyncio
async def test_generate_coaching_preference_wins_over_incidental_description_language(
    monkeypatch,
):
    """A stored language preference is authoritative even when the goal's
    free-text description happens to be written in a different language.
    """
    payload = '{"progress_assessment": "ok", "deposit_schedule": [], "nudges": [], "confidence": "high"}'
    fake_client = FakeClient(payload)
    monkeypatch.setattr(prometheus, "get_client", lambda: fake_client)
    monkeypatch.setattr(prometheus.anthropic.types, "TextBlock", DummyTextBlock, raising=False)

    await prometheus.generate_coaching(
        CoachingRequest(
            goal=SavingsGoalContext(
                target_amount=1000,
                currency="USDC",
                deadline="2026-12-31T00:00:00Z",
                description="Combien pour mon épargne et le rendement?",
                current_amount=200,
                progress_pct=20,
            ),
            portfolio=PortfolioContext(total_balance_usd=500),
            language="ha",
        )
    )

    sent_system = fake_client.messages.last_kwargs["system"]
    assert "Respond natively in Hausa" in sent_system
    assert "Respond natively in French" not in sent_system
