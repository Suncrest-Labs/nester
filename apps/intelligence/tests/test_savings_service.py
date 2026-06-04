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
