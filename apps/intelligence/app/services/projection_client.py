"""Pluggable provider for goal-success projections (#847 <-> #843 integration).

The Monte Carlo forecasting engine (#843, Go side:
`apps/api/internal/domain/projection/simulation.go` /
`service/projection_simulation.go`) exposes `POST /api/v1/tools/simulation`,
returning P10/P50/P90 bands and, when a target amount + deadline are given, a
`goal_success.probability` figure. As of this writing #843 is an open PR
against `dev` (not yet merged) -- this module calls the route defensively so
the recommendation engine works whether or not it has landed yet: any
failure (network error, 404 because the route isn't deployed yet, auth
failure, unexpected shape) returns None and callers degrade to the simpler
deterministic heuristic already computed in `recommendation_engine.py`
instead of blocking or inventing a substitute number.

To compute a "how much does raising the contribution help" delta without
needing to know the Go service's internal sensitivity-grid step sizes, this
calls the endpoint twice with the same goal (target/deadline/APY) and two
different `monthly_contribution` values -- once at the user's current
contribution rate, once at the proposed (higher) rate -- and takes the
difference of the two `goal_success.probability` values. Two round-trips,
but each is a fast in-process Monte Carlo run (#843 documents it as
low-single-digit milliseconds) and this keeps the Python side decoupled from
the Go service's internal grid-step choices.
"""

from __future__ import annotations

import logging
from typing import Any, Optional, Protocol

import aiohttp

logger = logging.getLogger(__name__)

_SIMULATION_PATH = "/api/v1/tools/simulation"


class ProjectionProvider(Protocol):
    async def fetch_goal_projection(
        self,
        goal_id: str,
        *,
        initial_deposit: float,
        current_monthly_contribution: float,
        required_monthly_contribution: float,
        apy: float,
        period_months: int,
        target_amount: float,
        deadline_months: int,
    ) -> Optional[dict[str, Any]]: ...


class ApiProjectionProvider:
    """Calls the #843 Monte Carlo simulation endpoint on the Go API.

    Follows the same aiohttp request pattern as
    `app.services.vault_context.VaultContextFetcher`.
    """

    def __init__(self, api_base_url: str, service_api_key: str) -> None:
        self.api_base_url = api_base_url.rstrip("/")
        self.service_api_key = service_api_key

    async def _simulate(
        self,
        *,
        goal_id: str,
        initial_deposit: float,
        monthly_contribution: float,
        apy: float,
        period_months: int,
        target_amount: float,
        deadline_months: int,
    ) -> Optional[float]:
        """One call to POST /api/v1/tools/simulation. Returns
        goal_success.probability, or None on any failure/unexpected shape."""
        url = f"{self.api_base_url}{_SIMULATION_PATH}"
        headers = {
            "Authorization": f"Bearer {self.service_api_key}",
            "Content-Type": "application/json",
        }
        payload = {
            "goal_id": goal_id,
            "initial_deposit": f"{initial_deposit:.2f}",
            "monthly_contribution": f"{monthly_contribution:.2f}",
            "apy": f"{apy:.6f}",
            "period_months": period_months,
            "compound_frequency": "monthly",
            "target_amount": f"{target_amount:.2f}",
            "deadline_months": deadline_months,
        }
        try:
            async with aiohttp.ClientSession() as session:
                async with session.post(
                    url, headers=headers, json=payload, timeout=aiohttp.ClientTimeout(total=5)
                ) as response:
                    if response.status != 200:
                        logger.debug(
                            "simulation unavailable for goal %s: status %s",
                            goal_id,
                            response.status,
                        )
                        return None
                    body = await response.json()
                    data = body.get("data") if isinstance(body, dict) else None
                    if not isinstance(data, dict):
                        return None
                    goal_success = data.get("goal_success")
                    if not isinstance(goal_success, dict):
                        return None
                    return float(goal_success["probability"])
        except Exception as exc:
            logger.debug("simulation fetch failed for goal %s: %s", goal_id, exc)
            return None

    async def fetch_goal_projection(
        self,
        goal_id: str,
        *,
        initial_deposit: float,
        current_monthly_contribution: float,
        required_monthly_contribution: float,
        apy: float,
        period_months: int,
        target_amount: float,
        deadline_months: int,
    ) -> Optional[dict[str, Any]]:
        """Return {"success_probability_delta": float} -- the real,
        Monte-Carlo-computed change in goal-success probability from raising
        the monthly contribution from current to required -- or None if the
        simulation endpoint isn't reachable/available.

        Never invents a substitute number: both legs must succeed, or this
        returns None and the caller keeps its heuristic estimate.
        """
        if not goal_id or period_months <= 0 or target_amount <= 0:
            return None

        p_current = await self._simulate(
            goal_id=goal_id,
            initial_deposit=initial_deposit,
            monthly_contribution=current_monthly_contribution,
            apy=apy,
            period_months=period_months,
            target_amount=target_amount,
            deadline_months=deadline_months,
        )
        if p_current is None:
            return None
        p_required = await self._simulate(
            goal_id=goal_id,
            initial_deposit=initial_deposit,
            monthly_contribution=required_monthly_contribution,
            apy=apy,
            period_months=period_months,
            target_amount=target_amount,
            deadline_months=deadline_months,
        )
        if p_required is None:
            return None
        return {"success_probability_delta": p_required - p_current}


_default_provider: Optional[ApiProjectionProvider] = None


def get_projection_provider() -> ApiProjectionProvider:
    global _default_provider
    if _default_provider is None:
        from app.config import settings

        _default_provider = ApiProjectionProvider(
            api_base_url=settings.nester_api_base_url,
            service_api_key=settings.nester_service_api_key,
        )
    return _default_provider
