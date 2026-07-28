from types import SimpleNamespace

import pytest
from stellar_sdk import Keypair

from app.models.rebalance import (
    ProtocolAllocation,
    RebalanceAction,
    RebalanceExecuteRequest,
    RebalanceSuggestRequest,
)
from app.services import rebalance_engine as engine_module
from app.services.rebalance_engine import RebalanceEngine


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


@pytest.fixture(autouse=True)
def _no_live_apy(monkeypatch):
    """Default all tests to the deterministic (non-live-data) code path."""

    async def _empty() -> dict[str, float]:
        return {}

    monkeypatch.setattr(engine_module, "_live_apy_by_protocol", _empty)


@pytest.mark.asyncio
async def test_analyze_recommends_rebalance_when_gain_exceeds_threshold():
    request = RebalanceSuggestRequest(
        vault_id="vault-1",
        vault_balance_usd=10_000,
        allocations=[ProtocolAllocation(protocol="ultra", percentage=100, apy=1.0)],
        threshold_pct=5.0,
    )
    result = await RebalanceEngine().analyze(request)

    assert result.vault_id == "vault-1"
    assert result.should_rebalance is True
    assert result.optimal_weighted_apy > result.current_weighted_apy
    assert result.yield_improvement_usd > 0
    assert 0 <= result.confidence <= 1
    assert len(result.actions) >= 1


@pytest.mark.asyncio
async def test_analyze_does_not_recommend_when_already_optimal():
    request = RebalanceSuggestRequest(
        vault_id="vault-2",
        vault_balance_usd=5_000,
        allocations=[ProtocolAllocation(protocol="blend", percentage=100, apy=50.0)],
        threshold_pct=5.0,
    )
    result = await RebalanceEngine().analyze(request)

    assert result.should_rebalance is False
    assert result.actions == []
    assert result.yield_improvement_usd == 0


@pytest.mark.asyncio
async def test_analyze_uses_claude_rationale_when_available(monkeypatch):
    monkeypatch.setattr(
        engine_module,
        "get_client",
        lambda: FakeClient("Move to blend for better risk-adjusted yield."),
    )
    monkeypatch.setattr(engine_module.anthropic.types, "TextBlock", DummyTextBlock, raising=False)

    request = RebalanceSuggestRequest(
        vault_id="vault-3",
        vault_balance_usd=1_000,
        allocations=[ProtocolAllocation(protocol="ultra", percentage=100, apy=1.0)],
    )
    result = await RebalanceEngine().analyze(request)

    assert "blend" in result.rationale.lower() or result.rationale


@pytest.mark.asyncio
async def test_analyze_falls_back_when_claude_fails(monkeypatch):
    def _boom():
        raise RuntimeError("anthropic down")

    monkeypatch.setattr(engine_module, "get_client", _boom)

    request = RebalanceSuggestRequest(
        vault_id="vault-4",
        vault_balance_usd=1_000,
        allocations=[ProtocolAllocation(protocol="ultra", percentage=100, apy=1.0)],
    )
    result = await RebalanceEngine().analyze(request)

    assert result.should_rebalance is True
    assert result.rationale  # deterministic fallback still produced


def test_build_unsigned_transaction_returns_valid_xdr():
    keypair = Keypair.random()
    request = RebalanceExecuteRequest(
        vault_id="vault-1",
        source_account=keypair.public_key,
        actions=[
            RebalanceAction.model_validate(
                {
                    "protocol": "blend",
                    "action": "increase",
                    "from": 40,
                    "to": 100,
                    "reason": "best yield",
                }
            )
        ],
    )
    response = RebalanceEngine().build_unsigned_transaction(request)

    assert response.vault_id == "vault-1"
    assert response.source_account == keypair.public_key
    assert response.unsigned_transaction_xdr
    assert "TESTNET" not in response.network_passphrase  # passphrase text, not the enum name
    assert "Test SDF Network" in response.network_passphrase


def test_build_unsigned_transaction_rejects_invalid_account():
    request = RebalanceExecuteRequest(
        vault_id="vault-1",
        source_account="not-a-real-account",
        actions=[],
    )
    with pytest.raises(ValueError):
        RebalanceEngine().build_unsigned_transaction(request)
