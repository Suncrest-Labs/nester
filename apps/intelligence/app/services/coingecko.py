"""CoinGecko client for live price data and market sentiment."""
import json
import logging
import time
from dataclasses import dataclass
from typing import Any

import aiohttp

logger = logging.getLogger(__name__)

_COINGECKO_BASE = "https://api.coingecko.com/api/v3"
_TTL_PRICES = 300      # 5 min
_TTL_SENTIMENT = 300

_redis_client: Any = None
_redis_available: bool = False
_mem_cache: dict[str, tuple[Any, float]] = {}


@dataclass
class PriceData:
    usd: float
    usd_24h_change: float
    usd_market_cap: float


@dataclass
class MarketSentiment:
    signal: str               # "bull" | "bear" | "neutral"
    defi_market_cap_usd: float
    defi_dominance_pct: float


def _get_redis() -> Any:
    global _redis_client, _redis_available
    if _redis_client is not None:
        return _redis_client if _redis_available else None
    try:
        import redis as _redis

        from app.config import settings
        _redis_client = _redis.from_url(settings.redis_url, decode_responses=True)
        _redis_client.ping()
        _redis_available = True
        logger.info("coingecko cache: redis connected")
    except Exception as exc:
        logger.warning("coingecko cache: redis unavailable (%s), using in-memory", exc)
        _redis_available = False
    return _redis_client if _redis_available else None


def _cache_get(key: str) -> Any | None:
    r = _get_redis()
    if r is not None:
        try:
            raw = r.get(key)
            if raw:
                return json.loads(raw)
        except Exception as exc:
            logger.warning("coingecko cache get: %s", exc)
    entry = _mem_cache.get(key)
    if entry and time.monotonic() < entry[1]:
        return entry[0]
    return None


def _cache_set(key: str, value: Any, ttl: int) -> None:
    r = _get_redis()
    if r is not None:
        try:
            r.setex(key, ttl, json.dumps(value))
            return
        except Exception as exc:
            logger.warning("coingecko cache set: %s", exc)
    _mem_cache[key] = (value, time.monotonic() + ttl)


class CoinGeckoClient:
    def __init__(self, base_url: str = _COINGECKO_BASE):
        self.base_url = base_url.rstrip("/")

    async def get_prices(self, coin_ids: list[str]) -> dict[str, PriceData]:
        """Fetch USD price, 24h change, and market cap. Returns last cached value on 429."""
        cache_key = f"coingecko:prices:{','.join(sorted(coin_ids))}"
        cached = _cache_get(cache_key)
        if cached is not None:
            return {k: PriceData(**v) for k, v in cached.items()}

        params = {
            "ids": ",".join(coin_ids),
            "vs_currencies": "usd",
            "include_24hr_change": "true",
            "include_market_cap": "true",
        }
        try:
            async with aiohttp.ClientSession() as session:
                async with session.get(
                    f"{self.base_url}/simple/price",
                    params=params,
                    timeout=aiohttp.ClientTimeout(total=8),
                ) as resp:
                    if resp.status == 429:
                        logger.warning("CoinGecko rate limited (429), returning empty prices")
                        return {}
                    if resp.status != 200:
                        logger.warning("CoinGecko /simple/price returned %s", resp.status)
                        return {}
                    data = await resp.json()
        except Exception as exc:
            logger.warning("coingecko get_prices failed: %s", exc)
            return {}

        result: dict[str, Any] = {}
        for coin_id in coin_ids:
            entry = data.get(coin_id, {})
            result[coin_id] = {
                "usd": float(entry.get("usd", 0.0) or 0.0),
                "usd_24h_change": float(entry.get("usd_24h_change", 0.0) or 0.0),
                "usd_market_cap": float(entry.get("usd_market_cap", 0.0) or 0.0),
            }

        _cache_set(cache_key, result, _TTL_PRICES)
        return {k: PriceData(**v) for k, v in result.items()}

    async def get_market_sentiment(self) -> MarketSentiment:
        """Fetch DeFi global market cap and dominance. Returns neutral on 429."""
        cache_key = "coingecko:defi_global"
        cached = _cache_get(cache_key)
        if cached is not None:
            return MarketSentiment(**cached)

        _neutral = MarketSentiment(
            signal="neutral",
            defi_market_cap_usd=0.0,
            defi_dominance_pct=0.0,
        )

        try:
            async with aiohttp.ClientSession() as session:
                async with session.get(
                    f"{self.base_url}/global/decentralized_finance_defi",
                    timeout=aiohttp.ClientTimeout(total=8),
                ) as resp:
                    if resp.status == 429:
                        logger.warning("CoinGecko rate limited (429), returning neutral sentiment")
                        return _neutral
                    if resp.status != 200:
                        logger.warning("CoinGecko /global/defi returned %s", resp.status)
                        return _neutral
                    data = await resp.json()
        except Exception as exc:
            logger.warning("coingecko get_market_sentiment failed: %s", exc)
            return _neutral

        defi_data = data.get("data", {})
        market_cap = float(defi_data.get("defi_market_cap", 0) or 0)
        dominance = float(defi_data.get("defi_dominance", 0) or 0)
        trading_vol = float(defi_data.get("trading_volume_24h", 0) or 0)

        signal = "neutral"
        if market_cap > 0:
            vol_ratio = trading_vol / market_cap
            if vol_ratio > 0.05:
                signal = "bull"
            elif vol_ratio < 0.02:
                signal = "bear"

        sentiment = MarketSentiment(
            signal=signal,
            defi_market_cap_usd=market_cap,
            defi_dominance_pct=dominance,
        )
        _cache_set(
            cache_key,
            {
                "signal": sentiment.signal,
                "defi_market_cap_usd": sentiment.defi_market_cap_usd,
                "defi_dominance_pct": sentiment.defi_dominance_pct,
            },
            _TTL_SENTIMENT,
        )
        return sentiment


_default_client: CoinGeckoClient | None = None


def get_client() -> CoinGeckoClient:
    global _default_client
    if _default_client is None:
        _default_client = CoinGeckoClient()
    return _default_client
