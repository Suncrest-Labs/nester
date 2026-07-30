import json
import logging
import time
from typing import Any, Optional

import aiohttp

logger = logging.getLogger(__name__)

_DEFILLAMA_BASE = "https://api.llama.fi"
_YIELDS_BASE = "https://yields.llama.fi"

_TTL_PROTOCOLS = 900  # 15 min
_TTL_POOLS = 900
_TTL_HISTORY = 900

# Staleness guard (#931): every successful fetch also writes a "last known
# good" copy under a much longer TTL. If a live fetch fails *and* the normal
# (short-TTL) cache has already expired, that stale copy is served instead of
# an empty list — a DefiLlama outage then degrades to slightly-out-of-date
# yield data instead of the AI having no yield data to reference at all.
# Capped at 24h: past that, the data is old enough that presenting it as
# current would be misleading, so it's better to fall back to empty (the
# existing failure behavior) than serve day-plus-old numbers silently.
_TTL_STALE_FALLBACK = 24 * 3600
_STALE_KEY_SUFFIX = ":stale"

_redis_client: Any = None
_redis_available: bool = False
_mem_cache: dict[str, tuple[Any, float]] = {}


def _get_redis() -> Optional[Any]:
    global _redis_client, _redis_available
    if _redis_client is not None:
        return _redis_client if _redis_available else None
    try:
        import redis as _redis

        from app.config import settings
        _redis_client = _redis.from_url(settings.redis_url, decode_responses=True)
        _redis_client.ping()
        _redis_available = True
        logger.info("defillama cache: redis connected")
    except Exception as exc:
        logger.warning("defillama cache: redis unavailable (%s), using in-memory", exc)
        _redis_available = False
    return _redis_client if _redis_available else None


def _cache_get(key: str) -> Optional[Any]:
    r = _get_redis()
    if r is not None:
        try:
            raw = r.get(key)
            if raw:
                return json.loads(raw)
        except Exception as exc:
            logger.warning("defillama cache get: %s", exc)
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
            logger.warning("defillama cache set: %s", exc)
    _mem_cache[key] = (value, time.monotonic() + ttl)


def _cache_set_with_stale_fallback(key: str, value: Any, ttl: int) -> None:
    """Write the normal (short-TTL) cache entry plus a long-TTL "last known
    good" copy for the staleness guard to fall back on."""
    _cache_set(key, value, ttl)
    _cache_set(key + _STALE_KEY_SUFFIX, value, _TTL_STALE_FALLBACK)


def _cache_get_stale(key: str) -> Optional[Any]:
    """Read the long-TTL "last known good" copy, ignoring the normal cache's
    TTL entirely — used only as an outage fallback, never on the hot path."""
    return _cache_get(key + _STALE_KEY_SUFFIX)


def _stale_fallback_or_empty(key: str, what: str) -> list[Any]:
    stale = _cache_get_stale(key)
    if stale is not None:
        logger.warning(
            "defillama %s: live fetch failed, serving last-known-good cached data "
            "(up to %ds old) instead of empty",
            what,
            _TTL_STALE_FALLBACK,
        )
        return list(stale)
    return []


class DeFiLlamaClient:
    def __init__(self, base_url: str = _DEFILLAMA_BASE, yields_url: str = _YIELDS_BASE):
        self.base_url = base_url.rstrip("/")
        self.yields_url = yields_url.rstrip("/")

    async def get_stellar_protocols(self) -> list[dict[str, Any]]:
        key = "defillama:stellar_protocols"
        cached = _cache_get(key)
        if cached is not None:
            return list(cached)

        try:
            async with aiohttp.ClientSession() as session:
                async with session.get(
                    f"{self.base_url}/protocols",
                    timeout=aiohttp.ClientTimeout(total=10),
                ) as resp:
                    if resp.status != 200:
                        logger.warning("defillama /protocols returned %s", resp.status)
                        return _stale_fallback_or_empty(key, "get_stellar_protocols")
                    data = await resp.json()
        except Exception as exc:
            logger.warning("defillama get_stellar_protocols failed: %s", exc)
            return _stale_fallback_or_empty(key, "get_stellar_protocols")

        stellar = [
            {
                "name": p.get("name", ""),
                "slug": p.get("slug", ""),
                "tvl": p.get("tvl", 0),
                "chain": "Stellar",
                "category": p.get("category", ""),
            }
            for p in data
            if "stellar" in [c.lower() for c in (p.get("chains") or [])]
        ]
        _cache_set_with_stale_fallback(key, stellar, _TTL_PROTOCOLS)
        return stellar

    async def get_yield_pools(self, chain: str = "Stellar") -> list[dict[str, Any]]:
        key = f"defillama:pools:{chain.lower()}"
        cached = _cache_get(key)
        if cached is not None:
            return list(cached)

        try:
            async with aiohttp.ClientSession() as session:
                async with session.get(
                    f"{self.yields_url}/pools",
                    timeout=aiohttp.ClientTimeout(total=10),
                ) as resp:
                    if resp.status != 200:
                        logger.warning("defillama /pools returned %s", resp.status)
                        return _stale_fallback_or_empty(key, "get_yield_pools")
                    data = await resp.json()
        except Exception as exc:
            logger.warning("defillama get_yield_pools failed: %s", exc)
            return _stale_fallback_or_empty(key, "get_yield_pools")

        pools = [
            {
                "pool": p.get("pool", ""),
                "project": p.get("project", ""),
                "symbol": p.get("symbol", ""),
                "apy": p.get("apy") or 0.0,
                "apyBase": p.get("apyBase") or 0.0,
                "apyReward": p.get("apyReward") or 0.0,
                "tvlUsd": p.get("tvlUsd") or 0.0,
                "apyPct7d": p.get("apyPct7d"),
                "il7d": p.get("il7d"),
                "chain": p.get("chain", ""),
            }
            for p in data.get("data", [])
            if p.get("chain", "").lower() == chain.lower()
        ]
        _cache_set_with_stale_fallback(key, pools, _TTL_POOLS)
        return pools

    async def get_pool_history(self, pool_id: str) -> list[dict[str, Any]]:
        key = f"defillama:pool_history:{pool_id}"
        cached = _cache_get(key)
        if cached is not None:
            return list(cached)

        try:
            async with aiohttp.ClientSession() as session:
                async with session.get(
                    f"{self.yields_url}/chart/{pool_id}",
                    timeout=aiohttp.ClientTimeout(total=10),
                ) as resp:
                    if resp.status != 200:
                        logger.warning("defillama /chart/%s returned %s", pool_id, resp.status)
                        return _stale_fallback_or_empty(key, "get_pool_history")
                    data = await resp.json()
        except Exception as exc:
            logger.warning("defillama get_pool_history failed: %s", exc)
            return _stale_fallback_or_empty(key, "get_pool_history")

        history = [
            {
                "timestamp": entry.get("timestamp", ""),
                "apy": entry.get("apy") or 0.0,
                "tvlUsd": entry.get("tvlUsd") or 0.0,
            }
            for entry in data.get("data", [])
        ]
        _cache_set_with_stale_fallback(key, history, _TTL_HISTORY)
        return history


_default_client: Optional[DeFiLlamaClient] = None


def get_client() -> DeFiLlamaClient:
    global _default_client
    if _default_client is None:
        try:
            from app.config import settings
            base_url = getattr(settings, "defillama_base_url", None) or _DEFILLAMA_BASE
        except Exception:
            base_url = _DEFILLAMA_BASE
        _default_client = DeFiLlamaClient(base_url=base_url)
    return _default_client
