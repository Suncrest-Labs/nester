from unittest.mock import AsyncMock, patch

import pytest

from app.models.savings import SavingsPlanRequest
from app.services.savings_service import SavingsService


@pytest.mark.asyncio
async def test_savings_calculation():
    service = SavingsService()

    # Mock get_default_apy to return 12% (1% monthly)
    async def mock_apy():
        return 0.12

    service.get_default_apy = mock_apy

    request = SavingsPlanRequest(
        goal_usdc=1000,
        time_horizon_months=10,
        max_monthly_contribution_usdc=200,
    )

    plan = await service.generate_plan(request)

    # APY = 12%, Monthly r = 0.01
    # P = FV * (r / ((1 + r)^n - 1))
    # P = 1000 * (0.01 / ((1.01)^10 - 1))
    # P = 1000 * (0.01 / (1.104622 - 1))
    # P = 1000 * (0.01 / 0.104622)
    # P = 1000 * 0.095582
    # P = 95.58

    assert plan.required_monthly_deposit == 95.58
    assert plan.achievable is True
    assert len(plan.monthly_schedule) == 10
    assert plan.monthly_schedule[-1].expected_balance == 1000.0
    assert plan.total_yield_earned > 0


@pytest.mark.asyncio
async def test_achievability():
    service = SavingsService()

    async def mock_apy():
        return 0.0  # 0% APY for simple math

    service.get_default_apy = mock_apy

    request = SavingsPlanRequest(
        goal_usdc=1000,
        time_horizon_months=10,
        max_monthly_contribution_usdc=50,
    )

    plan = await service.generate_plan(request)

    # Required = 1000 / 10 = 100
    # Max = 50
    # Achievable = False

    assert plan.required_monthly_deposit == 100.0
    assert plan.achievable is False


# ---------------------------------------------------------------------------
# get_default_apy — the "APY recommendation" building block used whenever a
# caller doesn't pin a specific vault.
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_get_default_apy_averages_available_rates():
    service = SavingsService()
    service.fetcher.fetch_market_rates = AsyncMock(
        return_value=[
            {"protocol": "aave", "apy": 0.06},
            {"protocol": "blend", "apy": 0.09},
            {"protocol": "compound", "apy": 0.06},
        ]
    )

    apy = await service.get_default_apy()

    assert apy == pytest.approx(0.07)


@pytest.mark.asyncio
async def test_get_default_apy_falls_back_when_no_rates_available():
    service = SavingsService()
    service.fetcher.fetch_market_rates = AsyncMock(return_value=[])

    apy = await service.get_default_apy()

    assert apy == 0.08


# ---------------------------------------------------------------------------
# generate_plan — vault-specific APY selection
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_generate_plan_uses_matching_vault_rate_when_vault_id_given():
    service = SavingsService()
    service.fetcher.fetch_market_rates = AsyncMock(
        return_value=[
            {"protocol": "aave", "apy": 0.12},
            {"protocol": "blend", "apy": 0.06},
        ]
    )
    service._generate_narrative = AsyncMock(return_value="narrative")

    request = SavingsPlanRequest(
        goal_usdc=1000,
        time_horizon_months=10,
        max_monthly_contribution_usdc=200,
        vault_id="aave",
    )

    plan = await service.generate_plan(request)

    # Matches the 12% APY math from test_savings_calculation above.
    assert plan.required_monthly_deposit == 95.58
    assert plan.narrative == "narrative"


@pytest.mark.asyncio
async def test_generate_plan_falls_back_to_default_apy_when_vault_id_unmatched():
    service = SavingsService()
    service.fetcher.fetch_market_rates = AsyncMock(
        return_value=[
            {"protocol": "aave", "apy": 0.10},
            {"protocol": "blend", "apy": 0.10},
        ]
    )
    service._generate_narrative = AsyncMock(return_value="narrative")

    request = SavingsPlanRequest(
        goal_usdc=1000,
        time_horizon_months=10,
        max_monthly_contribution_usdc=200,
        vault_id="nonexistent-protocol",
    )

    plan = await service.generate_plan(request)

    # No rate matches "nonexistent-protocol", so get_default_apy() (the mean
    # of all available rates, 10%) is used instead of a KeyError/zero APY.
    assert plan.achievable is True
    assert plan.required_monthly_deposit > 0
    assert plan.monthly_schedule[-1].expected_balance == pytest.approx(1000.0, abs=0.5)


# ---------------------------------------------------------------------------
# _generate_narrative — the "progress summary" building block
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_generate_narrative_uses_claude_response_when_available():
    service = SavingsService()
    request = SavingsPlanRequest(
        goal_usdc=1000,
        time_horizon_months=10,
        max_monthly_contribution_usdc=200,
    )

    mock_text_block = AsyncMock()
    mock_text_block.text = "You're on track to hit your goal comfortably."
    mock_response = AsyncMock()
    mock_response.content = [mock_text_block]

    with patch(
        "app.services.savings_service.anthropic_client.messages.create",
        return_value=mock_response,
    ):
        narrative = await service._generate_narrative(
            request, apy=0.12, required_deposit=95.58, achievable=True, total_yield=45.8
        )

    assert narrative == "You're on track to hit your goal comfortably."


@pytest.mark.asyncio
async def test_generate_narrative_falls_back_when_claude_call_fails_achievable():
    service = SavingsService()
    request = SavingsPlanRequest(
        goal_usdc=1000,
        time_horizon_months=10,
        max_monthly_contribution_usdc=200,
    )

    with patch(
        "app.services.savings_service.anthropic_client.messages.create",
        side_effect=Exception("Claude unavailable"),
    ):
        narrative = await service._generate_narrative(
            request, apy=0.12, required_deposit=95.58, achievable=True, total_yield=45.8
        )

    assert "95.58" in narrative
    assert "45.80" in narrative
    assert "12.0%" in narrative


@pytest.mark.asyncio
async def test_generate_narrative_falls_back_when_claude_call_fails_not_achievable():
    service = SavingsService()
    request = SavingsPlanRequest(
        goal_usdc=1000,
        time_horizon_months=10,
        max_monthly_contribution_usdc=50,
    )

    with patch(
        "app.services.savings_service.anthropic_client.messages.create",
        side_effect=Exception("Claude unavailable"),
    ):
        narrative = await service._generate_narrative(
            request, apy=0.0, required_deposit=100.0, achievable=False, total_yield=0.0
        )

    assert "1000" in narrative
    assert "100.00" in narrative
    assert "above your limit" in narrative
