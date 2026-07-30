"""Concrete, API-backed DataSource for the retrieval layer (#852).

Adapts the existing VaultContextFetcher (vaults, available vaults, market rates)
and adds user-scoped savings-goal and transaction fetches against the Nester API.
Every user fetch sends the user_id the caller passed (from the JWT subject) — the
service key authenticates the intelligence service, and the user scope is fixed
by the caller, so a query can never retrieve another user's data.
"""

from __future__ import annotations

import logging
from typing import Any

import aiohttp

from app.services.retrieval import DataSource
from app.services.vault_context import VaultContextFetcher

logger = logging.getLogger(__name__)

_TIMEOUT = aiohttp.ClientTimeout(total=5)


class ApiDataSource(DataSource):
    """DataSource implementation over the Nester REST API."""

    def __init__(self, api_base_url: str, service_api_key: str) -> None:
        self._base = api_base_url.rstrip("/")
        self._key = service_api_key
        self._fetcher = VaultContextFetcher(api_base_url, service_api_key)

    def _headers(self, user_id: str | None = None) -> dict[str, str]:
        headers = {
            "Authorization": f"Bearer {self._key}",
            "Content-Type": "application/json",
        }
        if user_id:
            headers["X-User-Id"] = user_id
        return headers

    async def user_vaults(self, user_id: str) -> list[dict[str, Any]]:
        return await self._fetcher.fetch_user_vaults(user_id)

    async def available_vaults(self) -> list[dict[str, Any]]:
        return await self._fetcher.fetch_available_vaults()

    async def market_rates(self) -> list[dict[str, Any]]:
        return await self._fetcher.fetch_market_rates()

    async def savings_goals(self, user_id: str) -> list[dict[str, Any]]:
        url = f"{self._base}/api/v1/users/savings-goals"
        try:
            async with aiohttp.ClientSession() as session:
                async with session.get(
                    url, headers=self._headers(user_id), timeout=_TIMEOUT
                ) as resp:
                    if resp.status != 200:
                        return []
                    payload = await resp.json()
                    data = payload.get("data", payload) if isinstance(payload, dict) else payload
                    return list(data) if isinstance(data, list) else []
        except Exception as exc:  # noqa: BLE001 — degrade to no data, never raise into chat
            logger.warning("retrieval: savings goals fetch failed for %s: %s", user_id, exc)
            return []

    async def recent_transactions(self, user_id: str) -> list[dict[str, Any]]:
        url = f"{self._base}/api/v1/transactions"
        try:
            async with aiohttp.ClientSession() as session:
                async with session.get(
                    url,
                    headers=self._headers(user_id),
                    params={"limit": "10"},
                    timeout=_TIMEOUT,
                ) as resp:
                    if resp.status != 200:
                        return []
                    payload = await resp.json()
                    data = payload.get("data", payload) if isinstance(payload, dict) else payload
                    if isinstance(data, dict):
                        data = data.get("transactions", [])
                    return list(data) if isinstance(data, list) else []
        except Exception as exc:  # noqa: BLE001 — degrade to no data, never raise into chat
            logger.warning("retrieval: transactions fetch failed for %s: %s", user_id, exc)
            return []
