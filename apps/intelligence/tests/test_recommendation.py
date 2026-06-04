from types import SimpleNamespace

import pytest

from app.models.recommendation import VaultRecommendationRequest
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


class FakeVaultContextFetcher:
    async def fetch_available_vaults(self):
        return [
            {"id": "vault-1", "name": "Conservative Yield", "apy": 7.2},
            {"id": "vault-2", "name": "Balanced Growth", "apy": 11.4},
        ]

    async def fetch_market_rates(self):
        return [{"protocol": "blend", "apy": 0.09}]

    async def fetch_vault_risk(self, vault_id: str):
        if vault_id == "vault-1":
            return {"overall": 24.0, "tier": "low"}
        return {"overall": 52.0, "tier": "medium"}

    async def fetch_user_vaults(self, user_id: str):
        return [{"name": "Existing Vault", "balance_usd": 250.0, "apy": 6.8}]


@pytest.mark.asyncio
async def test_recommend_vaults_uses_live_data_and_claude(monkeypatch):
    payload = (
        '{"recommended_vaults": ['
        '{"vault_id": "vault-1", "allocation_pct": 70, '
        '"rationale": "Low risk and solid APY."}, '
        '{"vault_id": "vault-2", "allocation_pct": 30, '
        '"rationale": "Adds yield without overexposing the portfolio."}], '
        '"expected_yield_usdc": 112.0, "confidence": "high"}'
    )
    monkeypatch.setattr(prometheus, "get_client", lambda: FakeClient(payload))
    monkeypatch.setattr(prometheus, "get_vault_context_fetcher", lambda: FakeVaultContextFetcher())
    monkeypatch.setattr(prometheus.anthropic.types, "TextBlock", DummyTextBlock, raising=False)

    result = await prometheus.recommend_vaults(
        VaultRecommendationRequest(
            risk_tolerance="moderate",
            time_horizon_months=12,
            initial_deposit_usdc=1000,
            savings_goal="emergency fund",
        ),
        user_id="user-1",
    )

    assert result.confidence == "high"
    assert result.expected_yield_usdc == 112.0
    assert len(result.recommended_vaults) == 2
    assert result.recommended_vaults[0].vault_id == "vault-1"


@pytest.mark.asyncio
async def test_analyze_recommendation_returns_confidence_fields(monkeypatch):
    payload = (
        '{"action": "Shift 10% into Balanced Growth", '
        '"rationale": "The live APY is stronger and the risk score is still acceptable.", '
        '"confidence": "medium", '
        '"confidence_reason": "Live APY and risk scores were available.", '
        '"data_freshness": "vaults=live, risk scores=live, market rates=live", '
        '"disclaimer": "Guidance only."}'
    )
    monkeypatch.setattr(prometheus, "get_client", lambda: FakeClient(payload))
    monkeypatch.setattr(prometheus, "get_vault_context_fetcher", lambda: FakeVaultContextFetcher())
    monkeypatch.setattr(prometheus.anthropic.types, "TextBlock", DummyTextBlock, raising=False)

    result = await prometheus.analyze_recommendation("Which vault should I use?", "user-1")

    assert result.confidence in {"high", "medium", "low"}
    assert result.action.startswith("Shift 10%")
    assert result.confidence_reason
    assert result.data_freshness
