"""Tests for retrieval.py's relevance filtering and empty-result fallback (#930).

`RetrievalService.retrieve` gates which sections get fetched/included by the
intents `route_query` matches — that's this codebase's relevance filter (it
doesn't do numeric relevance scoring; a section is either routed-in or not).
These tests cover the two things #930 calls out that test_retrieval.py
(added alongside #852) didn't: that *irrelevant* sections are actually
excluded (not just that empty sources produce no data), and that a query
matching only one intent doesn't pull in unrelated data.
"""

import pytest

from app.services.retrieval import Intent, RetrievalService, route_query


class RecordingDataSource:
    """Records which source methods were actually called, so a test can
    assert an irrelevant one was never even fetched — not just that its
    section is absent from the output."""

    def __init__(self) -> None:
        self.called: set[str] = set()
        self.vaults = [{"name": "Growth Vault", "balance_usd": 1200, "apy": 8.5, "yield_earned": 30}]
        self.goals = [{"name": "Car", "target_amount": 5000, "current_amount": 1250, "progress_pct": 25}]
        self.txs = [{"type": "deposit", "amount": 500, "currency": "USDC", "created_at": "2026-06-01"}]
        self.available = [{"name": "Balanced", "apy": 10.0, "risk_tier": "medium"}]
        self.rates = [{"protocol": "aave", "apy": 6.5}]

    async def user_vaults(self, user_id: str):
        self.called.add("user_vaults")
        return self.vaults

    async def savings_goals(self, user_id: str):
        self.called.add("savings_goals")
        return self.goals

    async def recent_transactions(self, user_id: str):
        self.called.add("recent_transactions")
        return self.txs

    async def available_vaults(self):
        self.called.add("available_vaults")
        return self.available

    async def market_rates(self):
        self.called.add("market_rates")
        return self.rates


class TestRelevanceFiltering:
    @pytest.mark.asyncio
    async def test_goal_only_query_excludes_transactions_and_yield_landscape(self) -> None:
        src = RecordingDataSource()
        svc = RetrievalService(src)
        await svc.retrieve("user-1", "how is my car goal going?")

        assert "savings_goals" in src.called
        # POSITIONS is always fetched (Intent.GENERAL is always routed in
        # alongside whatever else matched — see route_query), but sources
        # only relevant to *other* intents must not be fetched at all.
        assert "recent_transactions" not in src.called
        assert "available_vaults" not in src.called
        assert "market_rates" not in src.called

    @pytest.mark.asyncio
    async def test_transactions_only_query_excludes_goals_and_yield_landscape(self) -> None:
        src = RecordingDataSource()
        svc = RetrievalService(src)
        await svc.retrieve("user-1", "show me my last deposits")

        assert "recent_transactions" in src.called
        assert "savings_goals" not in src.called
        assert "available_vaults" not in src.called
        assert "market_rates" not in src.called

    @pytest.mark.asyncio
    async def test_multi_intent_query_pulls_only_the_matched_sections(self) -> None:
        # A message that matches both GOALS and TRANSACTIONS keywords should
        # fetch both, but still not the unrelated YIELD_LANDSCAPE source.
        src = RecordingDataSource()
        svc = RetrievalService(src)
        await svc.retrieve("user-1", "how is my car goal doing, and what were my last deposits?")

        assert "savings_goals" in src.called
        assert "recent_transactions" in src.called
        assert "available_vaults" not in src.called
        assert "market_rates" not in src.called

    @pytest.mark.asyncio
    async def test_yield_landscape_query_excludes_goals_and_transactions(self) -> None:
        src = RecordingDataSource()
        svc = RetrievalService(src)
        await svc.retrieve("user-1", "which is the best vault right now?")

        assert "available_vaults" in src.called
        assert "market_rates" in src.called
        assert "savings_goals" not in src.called
        assert "recent_transactions" not in src.called


class TestEmptyResultFallback:
    @pytest.mark.asyncio
    async def test_matched_but_empty_source_yields_no_section_for_that_intent(self) -> None:
        # Goals intent matches, but the user has none — the section (and its
        # citation) must be omitted rather than rendered empty.
        src = RecordingDataSource()
        src.goals = []
        svc = RetrievalService(src)
        ctx = await svc.retrieve("user-1", "how is my car goal doing?")

        assert not any("Savings Goals" in s for s in ctx.sections)
        assert not any(c.source == "goals" for c in ctx.citations)

    @pytest.mark.asyncio
    async def test_all_sources_empty_falls_back_to_no_data_message(self) -> None:
        src = RecordingDataSource()
        src.vaults = []
        src.goals = []
        src.txs = []
        src.available = []
        src.rates = []
        svc = RetrievalService(src)
        ctx = await svc.retrieve("user-1", "how am I doing overall?")

        assert not ctx.has_data
        assert ctx.sections == []
        assert ctx.citations == []
        assert "No user data" in ctx.to_prompt_block()

    @pytest.mark.asyncio
    async def test_unmatched_query_still_defaults_to_positions_scope(self) -> None:
        # route_query's documented fallback: an unrecognized message defaults
        # to POSITIONS + GENERAL, not an empty intent set.
        intents = route_query("what's the weather like today?")
        assert Intent.POSITIONS in intents
        assert Intent.GENERAL in intents
        assert Intent.GOALS not in intents
        assert Intent.TRANSACTIONS not in intents
