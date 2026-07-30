import logging

from app.config import settings
from app.models.savings import (
    MilestoneProjection,
    SavingsPlanRequest,
    SavingsPlanResponse,
    ScheduleEntry,
)
from app.services.claude import build_system_prompt
from app.services.claude import client as anthropic_client
from app.services.finance_math import required_monthly_deposit as _required_monthly_deposit
from app.services.i18n import format_amount, format_percentage, resolve_language
from app.services.vault_context import VaultContextFetcher

logger = logging.getLogger(__name__)


class SavingsService:
    def __init__(self) -> None:
        self.fetcher = VaultContextFetcher(
            api_base_url=settings.nester_api_base_url,
            service_api_key=settings.nester_service_api_key,
        )

    async def get_default_apy(self) -> float:
        """Fetch market rates and return a reasonable default APY (e.g., mean of top protocols)."""
        rates = await self.fetcher.fetch_market_rates()
        if not rates:
            return 0.08  # Fallback to 8%

        # Simple average of available rates
        total_apy = sum(rate.get("apy", 0) for rate in rates)
        return float(total_apy / len(rates))

    async def generate_plan(self, request: SavingsPlanRequest) -> SavingsPlanResponse:
        # 1. Determine APY
        apy = 0.0
        if request.vault_id:
            # In a real scenario, we'd fetch specific vault APY.
            # For now, let's try to find it in market rates or user vaults if possible,
            # or just use a placeholder if it's a mock ID.
            # For this implementation, we'll fetch market rates and pick one or use default.
            rates = await self.fetcher.fetch_market_rates()
            vault_rate = next(
                (
                    rate
                    for rate in rates
                    if rate.get("protocol", "").lower() == request.vault_id.lower()
                ),
                None,
            )
            if vault_rate:
                apy = vault_rate.get("apy", 0)
            else:
                apy = await self.get_default_apy()
        else:
            apy = await self.get_default_apy()

        # 2. Calculate Required Monthly Deposit
        # FV = P * (((1 + r)^n - 1) / r) * (1 + r)  <-- Deposit at START of month
        # We'll use end of month for simplicity or consistent with standard calculators
        # FV = P * (((1 + r)^n - 1) / r)
        # P = FV * (r / ((1 + r)^n - 1))

        r = apy / 12
        n = request.time_horizon_months
        fv = request.goal_usdc

        required_deposit = _required_monthly_deposit(fv, r, n)

        achievable = required_deposit <= request.max_monthly_contribution_usdc

        # 3. Generate Schedule
        monthly_schedule = []
        current_balance = 0.0
        total_yield = 0.0
        milestones = []

        for month in range(1, n + 1):
            yield_this_month = current_balance * r
            total_yield += yield_this_month
            current_balance += yield_this_month + required_deposit

            monthly_schedule.append(
                ScheduleEntry(
                    month=month,
                    deposit=round(required_deposit, 2),
                    expected_balance=round(current_balance, 2),
                    yield_earned=round(total_yield, 2),
                )
            )

            if month % 6 == 0 or month == n:
                milestones.append(
                    MilestoneProjection(
                        month=month,
                        expected_balance=round(current_balance, 2),
                    )
                )

        # 4. Generate Narrative using Claude
        language = resolve_language(request.language)
        narrative = await self._generate_narrative(
            request, apy, required_deposit, achievable, total_yield, language
        )

        return SavingsPlanResponse(
            achievable=achievable,
            required_monthly_deposit=round(required_deposit, 2),
            monthly_schedule=monthly_schedule,
            total_yield_earned=round(total_yield, 2),
            narrative=narrative,
            milestones=milestones,
        )

    async def _generate_narrative(
        self,
        request: SavingsPlanRequest,
        apy: float,
        required_deposit: float,
        achievable: bool,
        total_yield: float,
        language: str,
    ) -> str:
        status_text = "achievable" if achievable else "NOT achievable"
        goal_str = format_amount(request.goal_usdc, "USDC", language)
        max_contribution_str = format_amount(
            request.max_monthly_contribution_usdc, "USDC", language
        )
        apy_str = format_percentage(apy * 100, language)
        required_deposit_str = format_amount(required_deposit, "USDC", language)
        total_yield_str = format_amount(total_yield, "USDC", language)
        prompt = (
            "You are Prometheus, a DeFi-savvy financial advisor.\n"
            f"A user wants to save {goal_str} in {request.time_horizon_months} months.\n"
            f"Their maximum monthly contribution is {max_contribution_str}.\n"
            f"The current applicable APY is {apy_str}.\n"
            f"The calculated required monthly deposit is {required_deposit_str}.\n"
            f"The goal is {status_text} within their stated contribution limit.\n"
            f"The total yield they will earn is {total_yield_str}.\n\n"
            "Provide a concise, encouraging narrative (2-3 sentences) explaining the plan.\n"
            "If it's achievable, highlight the power of compound interest and the yield "
            "they'll earn.\n"
            "If it's NOT achievable, suggest adjusting the time horizon, increasing the monthly "
            "contribution, or seeking a higher yield vault (while mentioning risk).\n"
            "Keep it professional yet conversational.\n"
            "Reference the amounts and percentages above exactly as given rather than "
            "reformatting them."
        )

        try:
            response = anthropic_client.messages.create(
                model=settings.anthropic_model,
                max_tokens=150,
                system=build_system_prompt(
                    "You are Prometheus, a DeFi-savvy financial advisor for Nester.",
                    language,
                ),
                messages=[{"role": "user", "content": prompt}],
            )
            if response.content and hasattr(response.content[0], "text"):
                return response.content[0].text
            return ""
        except Exception as e:
            logger.error(f"Error generating narrative from Claude: {e}")
            if achievable:
                return (
                    f"At the current {apy_str} APY, you need to deposit "
                    f"{required_deposit_str}/month. You'll earn {total_yield_str} "
                    "in interest along the way!"
                )
            return (
                f"To reach your goal of {goal_str}, you'd need to deposit "
                f"{required_deposit_str}/month, which is above your limit. "
                "Consider extending your timeline."
            )


savings_service: SavingsService = SavingsService()
