"""Unit tests for structured portfolio analysis with Claude tool use."""

from types import SimpleNamespace
from typing import Any

import pytest

from app.models.portfolio import (
    AllocationItem,
    PortfolioAnalysisResponse,
    PortfolioBreakdown,
)
from app.services import prometheus


class DummyTextBlock:
    """Mock text block for Claude responses."""

    def __init__(self, text: str) -> None:
        self.text = text


class DummyToolUseBlock:
    """Mock tool use block for Claude tool responses."""

    def __init__(self, tool_input: dict[str, Any]) -> None:
        self.type = "tool_use"
        self.input = tool_input


class FakeAsyncMessages:
    """Mock async messages client."""

    def __init__(
        self, payload: str | None = None, tool_input: dict[str, Any] | None = None
    ) -> None:
        self.payload = payload
        self.tool_input = tool_input

    async def create(self, *args: Any, **kwargs: Any) -> Any:
        """Return a mock response with either text or tool_use block."""
        content = []
        if self.payload:
            content.append(DummyTextBlock(self.payload))
        if self.tool_input:
            content.append(DummyToolUseBlock(self.tool_input))
        return SimpleNamespace(content=content)


class FakeAsyncClient:
    """Mock async Anthropic client."""

    def __init__(
        self, payload: str | None = None, tool_input: dict[str, Any] | None = None
    ) -> None:
        self.messages = FakeAsyncMessages(payload, tool_input)


class FakeVaultContextFetcher:
    """Mock vault context fetcher."""

    async def fetch_user_vaults(self, user_id: str) -> list[dict[str, Any]]:
        """Return mock user vaults."""
        return [
            {
                "id": "vault-1",
                "name": "Conservative Yield",
                "balance_usd": 5000.0,
                "apy": 5.2,
                "yield_earned": 21.67,
            },
            {
                "id": "vault-2",
                "name": "Balanced Growth",
                "balance_usd": 8000.0,
                "apy": 11.4,
                "yield_earned": 76.0,
            },
        ]

    async def fetch_market_rates(self) -> list[dict[str, Any]]:
        """Return mock market rates."""
        return [
            {"protocol": "blend", "apy": 0.09},
            {"protocol": "aave", "apy": 0.085},
        ]

    async def fetch_vault_risk(self, vault_id: str) -> dict[str, Any]:
        """Return mock vault risk scores."""
        if vault_id == "vault-1":
            return {"overall": 24.0, "tier": "low"}
        return {"overall": 52.0, "tier": "medium"}


@pytest.mark.asyncio
async def test_analyze_portfolio_with_tool_use(monkeypatch: Any) -> None:
    """Test portfolio analysis with Claude tool use response."""
    tool_input = {
        "total_value_usdc": 13000.0,
        "yield_30d_usdc": 97.67,
        "allocation_breakdown": [
            {"protocol": "Conservative Yield", "weight": 38.46, "apy": 5.2},
            {"protocol": "Balanced Growth", "weight": 61.54, "apy": 11.4},
        ],
        "risk_level": "moderate",
        "top_recommendation": "Consider increasing allocation to Balanced Growth for higher yield.",
        "rebalance_suggested": False,
    }

    monkeypatch.setattr(
        prometheus, "get_client", lambda: FakeAsyncClient(tool_input=tool_input)
    )
    monkeypatch.setattr(
        prometheus, "get_vault_context_fetcher", lambda: FakeVaultContextFetcher()
    )

    result = await prometheus.analyze_portfolio(user_id="user-123")

    # Verify response structure
    assert isinstance(result, PortfolioAnalysisResponse)
    assert result.analysis.total_value_usdc == 13000.0
    assert result.analysis.yield_30d_usdc == 97.67
    assert result.analysis.risk_level == "moderate"
    assert result.confidence == "high"
    assert len(result.analysis.allocation_breakdown) == 2

    # Verify allocation items
    alloc1 = result.analysis.allocation_breakdown[0]
    assert alloc1.protocol == "Conservative Yield"
    assert alloc1.weight == pytest.approx(38.46)
    assert alloc1.apy == 5.2

    alloc2 = result.analysis.allocation_breakdown[1]
    assert alloc2.protocol == "Balanced Growth"
    assert alloc2.weight == pytest.approx(61.54)
    assert alloc2.apy == 11.4

    # Verify narrative is generated
    assert "portfolio" in result.narrative.lower()
    assert "$13,000" in result.narrative or "13000" in result.narrative


@pytest.mark.asyncio
async def test_analyze_portfolio_empty_vaults(monkeypatch: Any) -> None:
    """Test portfolio analysis with no vaults."""

    class EmptyVaultFetcher:
        async def fetch_user_vaults(self, user_id: str) -> list[dict[str, Any]]:
            return []

        async def fetch_market_rates(self) -> list[dict[str, Any]]:
            return []

    monkeypatch.setattr(
        prometheus, "get_vault_context_fetcher", lambda: EmptyVaultFetcher()
    )

    result = await prometheus.analyze_portfolio(user_id="user-456")

    assert isinstance(result, PortfolioAnalysisResponse)
    assert result.analysis.total_value_usdc == 0.0
    assert result.analysis.yield_30d_usdc == 0.0
    assert len(result.analysis.allocation_breakdown) == 0
    assert result.confidence == "high"
    assert "don't have any active" in result.narrative.lower()


@pytest.mark.asyncio
async def test_analyze_portfolio_fallback_on_tool_error(monkeypatch: Any) -> None:
    """Test portfolio analysis falls back gracefully if tool use fails."""

    class ErrorClient:
        class Messages:
            async def create(self, *args: Any, **kwargs: Any) -> Any:
                raise RuntimeError("Claude API error")

        messages = Messages()

    monkeypatch.setattr(prometheus, "get_client", lambda: ErrorClient())  # type: ignore[assignment]
    monkeypatch.setattr(
        prometheus, "get_vault_context_fetcher", lambda: FakeVaultContextFetcher()
    )

    result = await prometheus.analyze_portfolio(user_id="user-789")

    # Should still return valid response with fallback data
    assert isinstance(result, PortfolioAnalysisResponse)
    assert result.analysis.total_value_usdc == 13000.0  # From mock vaults
    assert result.confidence == "medium"  # Fallback confidence
    assert len(result.analysis.allocation_breakdown) == 2


@pytest.mark.asyncio
async def test_analyze_portfolio_pydantic_validation(monkeypatch: Any) -> None:
    """Test that tool response is properly validated by Pydantic."""
    # Minimal valid tool input
    tool_input = {
        "total_value_usdc": 1000.0,
        "yield_30d_usdc": 10.0,
        "allocation_breakdown": [
            {"protocol": "Aave", "weight": 100.0, "apy": 8.5},
        ],
        "risk_level": "conservative",
        "top_recommendation": "Hold",
        "rebalance_suggested": False,
    }

    monkeypatch.setattr(
        prometheus, "get_client", lambda: FakeAsyncClient(tool_input=tool_input)
    )
    monkeypatch.setattr(
        prometheus, "get_vault_context_fetcher", lambda: FakeVaultContextFetcher()
    )

    result = await prometheus.analyze_portfolio(user_id="user-xyz")

    # Verify Pydantic models are properly constructed
    assert isinstance(result.analysis, PortfolioBreakdown)
    assert isinstance(result.analysis.allocation_breakdown[0], AllocationItem)
    assert result.analysis.allocation_breakdown[0].weight == 100.0
    assert result.analysis.risk_level == "conservative"


@pytest.mark.asyncio
async def test_analyze_portfolio_response_model_serialization() -> None:
    """Test that PortfolioAnalysisResponse can be serialized to JSON."""
    response = PortfolioAnalysisResponse(
        analysis=PortfolioBreakdown(
            total_value_usdc=5000.0,
            yield_30d_usdc=50.0,
            allocation_breakdown=[
                AllocationItem(protocol="Blend", weight=100.0, apy=10.5),
            ],
            risk_level="moderate",
            top_recommendation="Test recommendation",
            rebalance_suggested=False,
        ),
        narrative="Test narrative",
        confidence="high",
    )

    # Should serialize to dict without errors
    serialized = response.model_dump()
    assert serialized["analysis"]["total_value_usdc"] == 5000.0
    assert serialized["narrative"] == "Test narrative"
    assert serialized["confidence"] == "high"
    assert len(serialized["analysis"]["allocation_breakdown"]) == 1


@pytest.mark.asyncio
async def test_analyze_portfolio_allocation_weighting(monkeypatch: Any) -> None:
    """Test that allocation weights are calculated correctly."""
    tool_input = {
        "total_value_usdc": 10000.0,
        "yield_30d_usdc": 100.0,
        "allocation_breakdown": [
            {"protocol": "Conservative", "weight": 30.0, "apy": 5.0},
            {"protocol": "Growth", "weight": 70.0, "apy": 12.0},
        ],
        "risk_level": "moderate",
        "top_recommendation": "Rebalance into higher yield",
        "rebalance_suggested": True,
    }

    monkeypatch.setattr(
        prometheus, "get_client", lambda: FakeAsyncClient(tool_input=tool_input)
    )
    monkeypatch.setattr(
        prometheus, "get_vault_context_fetcher", lambda: FakeVaultContextFetcher()
    )

    result = await prometheus.analyze_portfolio(user_id="user-weights")

    # Verify weights
    assert sum(
        alloc.weight for alloc in result.analysis.allocation_breakdown
    ) == pytest.approx(100.0)
    assert result.analysis.rebalance_suggested is True
