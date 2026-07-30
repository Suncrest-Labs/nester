"""Tests for retrieval relevance and intent classification (#852)."""

from app.services.retrieval import Intent, RetrievalService, classify_intent


class RecordingDataSource:
    """Records which endpoints were called, returns canned test data."""

    def __init__(self) -> None:
        self.called: set[str] = set()
        self.vaults = [
            {"name": "Growth Vault", "balance_usd": 1200, "apy": 8.5, "yield_earned": 30}
        ]
        self.goals = [
            {"name": "Car", "target_amount": 5000, "current_amount": 1250, "progress_pct": 25}
        ]
        self.txs = [
            {"type": "deposit", "amount": 500, "currency": "USDC", "created_at": "2026-06-01"}
        ]
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


class TestIntentClassification:
    def test_goals_intent(self):
        intents = classify_intent("I want to save for a car by next year")
        assert Intent.GOALS in intents

    def test_yield_intent(self):
        intents = classify_intent("Which vault has the highest APY?")
        assert Intent.YIELD_LANDSCAPE in intents

    def test_transactions_intent(self):
        intents = classify_intent("Show my recent deposits")
        assert Intent.TRANSACTIONS in intents

    def test_positions_intent(self):
        intents = classify_intent("What is my current balance?")
        assert Intent.POSITIONS in intents

    def test_mixed_intent(self):
        intents = classify_intent("How much do I have in my car savings goal?")
        assert Intent.GOALS in intents
        assert Intent.POSITIONS in intents

    def test_unknown_intent(self):
        intents = classify_intent("Hello, how are you?")
        assert Intent.UNKNOWN in intents


class TestRetrievalService:
    async def test_retrieve_goals_only(self):
        source = RecordingDataSource()
        service = RetrievalService(source)

        result = await service.retrieve("user-1", "How much saved for car?")
        assert "goals" in source.called
        assert "Car" in str(result.sections)
        assert "5000" in str(result.sections)

    async def test_retrieve_yield_landscape(self):
        source = RecordingDataSource()
        service = RetrievalService(source)

        result = await service.retrieve("user-1", "Which vault has best yield?")
        assert "available_vaults" in source.called
        assert "Balanced" in str(result.sections)
        assert "10.0" in str(result.sections)

    async def test_retrieve_transactions(self):
        source = RecordingDataSource()
        service = RetrievalService(source)

        result = await service.retrieve("user-1", "Show my recent activity")
        assert "recent_transactions" in source.called
        assert "deposit" in str(result.sections)

    async def test_retrieve_all_for_broad_question(self):
        source = RecordingDataSource()
        service = RetrievalService(source)

        _ = await service.retrieve("user-1", "Tell me about my savings")
        assert "user_vaults" in source.called
        assert "savings_goals" in source.called
        assert "recent_transactions" in source.called
        assert "available_vaults" in source.called

    async def test_retrieve_only_requested_intents(self):
        source = RecordingDataSource()
        service = RetrievalService(source)

        _ = await service.retrieve(
            "user-1",
            "What are my goals?",
            intents=[Intent.GOALS]
        )
        assert "savings_goals" in source.called
        assert "user_vaults" not in source.called
        assert "available_vaults" not in source.called

    async def test_retrieve_stores_numbers_for_grounding(self):
        source = RecordingDataSource()
        service = RetrievalService(source)

        result = await service.retrieve("user-1", "Show my vaults")
        assert result.numbers == [1200, 8.5, 30]
