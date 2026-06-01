"""Vault context fetcher for Prometheus AI."""
import logging
from typing import Any, Dict, List

try:
    from cachetools import TTLCache
    HAS_CACHETOOLS = True
except ImportError:
    HAS_CACHETOOLS = False
    logging.warning("cachetools not available, using simple dict cache")

import aiohttp

logger = logging.getLogger(__name__)


class VaultContextFetcher:
    def __init__(self, api_base_url: str, service_api_key: str):
        self.api_base_url = api_base_url.rstrip('/')
        self.service_api_key = service_api_key
        # Initialize market rates cache (TTL: 5 minutes)
        self._market_rates_cache: Any
        if HAS_CACHETOOLS:
            self._market_rates_cache = TTLCache(maxsize=100, ttl=300)  # 5 minutes
        else:
            self._market_rates_cache = {}
            self._market_rates_cache_expiry: float = 0.0

    async def fetch_user_vaults(self, user_id: str) -> List[Dict[str, Any]]:
        """
        Fetch user's vaults from Nester API.

        Returns list of vault dicts: {name, balance_usd, apy, allocation_breakdown}
        """
        url = f"{self.api_base_url}/api/v1/user-vaults/{user_id}"
        headers = {
            "Authorization": f"Bearer {self.service_api_key}",
            "Content-Type": "application/json"
        }

        try:
            async with aiohttp.ClientSession() as session:
                async with session.get(url, headers=headers) as response:
                    if response.status == 200:
                        payload = await response.json()
                        data = payload.get("data", payload) if isinstance(payload, dict) else {}
                        vault_rows = data.get("vaults", []) if isinstance(data, dict) else []
                        # Transform to expected format
                        vaults = []
                        for vault in vault_rows:
                            # Calculate allocation breakdown as percentages
                            total_balance = vault.get("total_balance_usd", 0)
                            allocation_breakdown = {}
                            if total_balance > 0:
                                for alloc in vault.get("allocations", []):
                                    protocol = alloc.get("protocol", "unknown")
                                    amount = alloc.get("amount_usd", 0)
                                    percentage = (amount / total_balance) * 100
                                    allocation_breakdown[protocol] = round(percentage, 2)

                            vaults.append({
                                "name": vault.get(
                                    "name", vault.get("contract_address", "Unknown Vault")
                                ),
                                "balance_usd": vault.get("total_balance_usd", 0),
                                "apy": vault.get("average_apy", 0),
                                "allocation_breakdown": allocation_breakdown,
                                "yield_earned": vault.get("yield_earned_usd", 0),
                                "lock_period_days": vault.get("lock_period_days", 0),
                                "id": vault.get("id", ""),
                            })
                        return vaults
                    else:
                        logger.error(f"Failed to fetch user vaults: {response.status}")
                        return []
        except Exception as e:
            logger.exception(f"Error fetching user vaults for user {user_id}: {e}")
            return []

    async def fetch_vault_risk(self, vault_id: str) -> Dict[str, Any]:
        """
        Fetch risk score for a specific vault from Nester API.

        Returns risk dict: {overall, tier, concentration_risk, protocol_risk,
        yield_volatility, liquidity_risk}
        """
        if not vault_id:
            return {}

        url = f"{self.api_base_url}/api/v1/vaults/{vault_id}/risk"
        headers = {
            "Authorization": f"Bearer {self.service_api_key}",
            "Content-Type": "application/json"
        }

        try:
            async with aiohttp.ClientSession() as session:
                async with session.get(url, headers=headers) as response:
                    if response.status == 200:
                        return dict(await response.json())
                    else:
                        logger.warning(
                            f"Failed to fetch risk for vault {vault_id}: {response.status}"
                        )
                        return {}
        except Exception as e:
            logger.warning(f"Error fetching risk for vault {vault_id}: {e}")
            return {}

    async def fetch_available_vaults(self) -> List[Dict[str, Any]]:
        """
        Fetch the current vault list from Nester API.

        Returns the live vault metadata used to rank recommendation options.
        """
        url = f"{self.api_base_url}/api/v1/vaults/all"
        headers = {
            "Authorization": f"Bearer {self.service_api_key}",
            "Content-Type": "application/json"
        }

        try:
            async with aiohttp.ClientSession() as session:
                async with session.get(url, headers=headers) as response:
                    if response.status != 200:
                        logger.warning(f"Failed to fetch vault list: {response.status}")
                        return []

                    data = await response.json()
                    raw_vaults = data.get("data") if isinstance(data, dict) else data
                    if not isinstance(raw_vaults, list):
                        raw_vaults = data.get("vaults", []) if isinstance(data, dict) else []

                    vaults: List[Dict[str, Any]] = []
                    for vault in raw_vaults:
                        if not isinstance(vault, dict):
                            continue
                        name = vault.get(
                            "name",
                            vault.get("contract_address", "Unknown Vault"),
                        )
                        balance = vault.get(
                            "current_balance",
                            vault.get("total_balance_usd", 0),
                        )
                        vaults.append({
                            "id": vault.get("id", ""),
                            "name": name,
                            "apy": vault.get("average_apy", vault.get("apy", 0)),
                            "balance_usd": balance,
                            "risk_tier": vault.get(
                                "risk_tier",
                                vault.get("status", "unknown"),
                            ),
                            "currency": vault.get("currency", "USDC"),
                        })
                    return vaults
        except Exception as e:
            logger.warning(f"Error fetching available vaults: {e}")
            return []

    async def fetch_market_rates(self) -> List[Dict[str, Any]]:
        """
        Fetch market rates from DefiLlama with caching and fallback.

        Returns list of dicts filtered for Aave, Blend, Compound entries.
        """
        # Check cache first
        if HAS_CACHETOOLS:
            cached = self._market_rates_cache.get("market_rates")
            if cached is not None:
                return list(cached)
        else:
            import time
            if time.time() < self._market_rates_cache_expiry and self._market_rates_cache.get(
                "data"
            ):
                return list(self._market_rates_cache["data"])

        # Fetch from DefiLlama
        url = "https://yields.llama.fi/pools"
        try:
            async with aiohttp.ClientSession() as session:
                async with session.get(url, timeout=aiohttp.ClientTimeout(total=10)) as response:
                    if response.status == 200:
                        data = await response.json()
                        pools = data.get("data", [])

                        # Filter for Aave, Blend, Compound
                        filtered_rates = []
                        target_protocols = {"aave", "blend", "compound"}

                        for pool in pools:
                            project = pool.get("project", "").lower()
                            if project in target_protocols:
                                filtered_rates.append({
                                    "protocol": project,
                                    "symbol": pool.get("symbol", ""),
                                    "apy": pool.get("apy", 0),
                                    "tvlUsd": pool.get("tvlUsd", 0),
                                    "chain": pool.get("chain", "")
                                })

                        # Cache the result
                        if HAS_CACHETOOLS:
                            self._market_rates_cache["market_rates"] = filtered_rates
                        else:
                            import time
                            self._market_rates_cache = {
                                "data": filtered_rates,
                                "expiry": time.time() + 300  # 5 minutes
                            }
                            self._market_rates_cache_expiry = float(time.time() + 300)

                        return filtered_rates
                    else:
                        logger.warning(f"DefiLlama API returned status {response.status}")
        except Exception as e:
            logger.warning(f"Failed to fetch from DefiLlama: {e}")

        # Fallback to hardcoded rates
        logger.warning("Using fallback market rates")
        fallback_rates = [
            {"protocol": "aave", "symbol": "aUSDC", "apy": 0.065, "tvlUsd": 0, "chain": "ethereum"},
            {
                "protocol": "blend",
                "symbol": "blendUSDC",
                "apy": 0.09,
                "tvlUsd": 0,
                "chain": "stellar",
            },
            {
                "protocol": "compound",
                "symbol": "cUSDC",
                "apy": 0.058,
                "tvlUsd": 0,
                "chain": "ethereum",
            },
        ]

        # Cache fallback too
        if HAS_CACHETOOLS:
            self._market_rates_cache["market_rates"] = fallback_rates
        else:
            import time
            self._market_rates_cache = {
                "data": fallback_rates,
                "expiry": time.time() + 300
            }
            self._market_rates_cache_expiry = float(time.time() + 300)

        return fallback_rates

    def build_context_block(
        self, vaults: List[Dict[str, Any]], market_rates: List[Dict[str, Any]]
    ) -> str:
        """
        Build a formatted string block to be injected into the system prompt.
        """
        if not vaults:
            vault_context = "The user has no active vaults."
        else:
            vault_lines = []
            for vault in vaults:
                name = vault.get("name", "Unknown")
                balance = vault.get("balance_usd", 0)
                apy = vault.get("apy", 0)
                allocation = vault.get("allocation_breakdown", {})

                alloc_str = (
                    ", ".join([f"{k}: {v}%" for k, v in allocation.items()])
                    if allocation
                    else "No allocation data"
                )

                vault_lines.append(
                    f"- {name}: ${balance:,.2f} balance, {apy:.2f}% APY, Allocation: [{alloc_str}]"
                )

            vault_context = f"""## User Portfolio
{chr(10).join(vault_lines)}"""

        if not market_rates:
            market_context = "## Current Market Rates (Live)\nMarket data unavailable."
        else:
            market_lines = []
            for rate in market_rates:
                protocol = rate.get("protocol", "unknown").upper()
                apy = rate.get("apy", 0)
                market_lines.append(f"- {protocol}: {apy * 100:.2f}% APY")

            market_context = f"""## Current Market Rates (Live)
{chr(10).join(market_lines)}"""

        return f"""{vault_context}

{market_context}"""

    def build_risk_profile_block(
        self, vaults: List[Dict[str, Any]], risk_data: Dict[str, Dict[str, Any]]
    ) -> str:
        """
        Build a risk profile block to be injected into the system prompt.
        """
        if not vaults:
            return "## Risk Profile\nNo vaults to assess risk."

        risk_lines = []
        for vault in vaults:
            vault_id = vault.get("id", "")
            vault_name = vault.get("name", vault.get("contract_address", "Unknown Vault"))

            # Get risk data for this vault
            vault_risk = risk_data.get(vault_id, {}) if risk_data else {}

            if not vault_risk:
                risk_lines.append(f"- {vault_name}: Risk data unavailable")
                continue

            overall_score = vault_risk.get("overall", 0)
            tier = vault_risk.get("tier", "unknown")

            # Find the primary driver (highest weighted dimension)
            # Weights: concentration 35%, protocol 30%, yield volatility 20%, liquidity 15%
            weighted_scores = {
                "concentration_risk": vault_risk.get("concentration_risk", 0) * 0.35,
                "protocol_risk": vault_risk.get("protocol_risk", 0) * 0.30,
                "yield_volatility": vault_risk.get("yield_volatility", 0) * 0.20,
                "liquidity_risk": vault_risk.get("liquidity_risk", 0) * 0.15
            }

            primary_driver = (
                max(weighted_scores, key=lambda k: weighted_scores[k])
                if weighted_scores
                else "unknown"
            )
            primary_driver_name = {
                "concentration_risk": "Concentration Risk",
                "protocol_risk": "Protocol Risk",
                "yield_volatility": "Yield Volatility",
                "liquidity_risk": "Liquidity Risk"
            }.get(primary_driver, "Unknown Factor")

            # Generate recommendation based on tier and primary driver
            recommendation = self._generate_risk_recommendation(tier, primary_driver, vault_risk)

            risk_lines.append(
                f"- {vault_name}: {tier} risk (score {overall_score:.0f}/100). "
                f"Primary driver: {primary_driver_name}. Recommendation: {recommendation}"
            )

        return f"""## Risk Profile
{chr(10).join(risk_lines)}"""

    def _generate_risk_recommendation(
        self, tier: str, primary_driver: str, risk_data: Dict[str, Any]
    ) -> str:
        """Generate a contextual rebalancing suggestion based on risk profile."""
        if tier == "low":
            return "Your portfolio is well-balanced. Consider maintaining current allocation."
        elif tier == "medium":
            if primary_driver == "concentration_risk":
                return (
                    "Consider diversifying across additional protocols to reduce "
                    "concentration risk."
                )
            elif primary_driver == "protocol_risk":
                return (
                    "Consider shifting allocation toward lower-risk protocols "
                    "like Aave or Compound."
                )
            elif primary_driver == "yield_volatility":
                return (
                    "Consider allocating to more stable vaults with lower "
                    "APY variability."
                )
            else:  # liquidity_risk
                return (
                    "Your vault size may be large relative to protocol market size. "
                    "Consider smaller positions."
                )
        else:  # high risk
            if primary_driver == "concentration_risk":
                return (
                    "Strongly consider diversifying across multiple protocols to "
                    "reduce concentration risk."
                )
            elif primary_driver == "protocol_risk":
                return (
                    "consider reallocating to lower-risk protocols to reduce "
                    "overall portfolio risk."
                )
            elif primary_driver == "yield_volatility":
                return (
                    "consider moving to more stable yield strategies to reduce "
                    "volatility exposure."
                )
            else:  # liquidity_risk
                return (
                    "consider reducing position size to lower liquidity risk or "
                    "spreading across protocols."
                )
