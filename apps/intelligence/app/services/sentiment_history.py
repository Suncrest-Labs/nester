"""Historical market sentiment tracking for the trend sparkline (#939).

Each time /market/sentiment is computed successfully, the resulting
signal/confidence is recorded here. The router exposes the recorded points so
the dapp can render a 7/30 day trend alongside the current point-in-time
read, instead of just the latest value.

Storage mirrors the pattern in coingecko.py: Redis when available (durable
across restarts, shared across replicas), falling back to an in-memory list
scoped to this process otherwise.
"""

import json
import logging
import time
from typing import Any

logger = logging.getLogger(__name__)

_REDIS_KEY = "market_sentiment_history"
_RETENTION_SECONDS = 30 * 24 * 60 * 60  # keep 30 days of points
_MEM_CAP = 500  # in-memory fallback cap, well above 30 days at a 6h poll cadence

_redis_client: Any = None
_redis_available: bool = False
_mem_history: list[dict[str, Any]] = []


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
        logger.info("sentiment history: redis connected")
    except Exception as exc:
        logger.warning("sentiment history: redis unavailable (%s), using in-memory", exc)
        _redis_available = False
    return _redis_client if _redis_available else None


def record(signal: str, confidence: float, observed_at: float | None = None) -> None:
    """Append one sentiment data point."""
    ts = observed_at if observed_at is not None else time.time()
    entry = {"signal": signal, "confidence": confidence, "observed_at": ts}

    r = _get_redis()
    if r is not None:
        try:
            r.zadd(_REDIS_KEY, {json.dumps(entry): ts})
            r.zremrangebyscore(_REDIS_KEY, 0, ts - _RETENTION_SECONDS)
            return
        except Exception as exc:
            logger.warning("sentiment history: redis record failed (%s)", exc)

    _mem_history.append(entry)
    cutoff = ts - _RETENTION_SECONDS
    _mem_history[:] = [e for e in _mem_history if e["observed_at"] >= cutoff]
    if len(_mem_history) > _MEM_CAP:
        del _mem_history[: len(_mem_history) - _MEM_CAP]


def history(days: int) -> list[dict[str, Any]]:
    """Return recorded points from the last `days` days, oldest first."""
    cutoff = time.time() - days * 24 * 60 * 60

    r = _get_redis()
    if r is not None:
        try:
            raw = r.zrangebyscore(_REDIS_KEY, cutoff, "+inf")
            points = [json.loads(item) for item in raw]
            points.sort(key=lambda e: e["observed_at"])
            return points
        except Exception as exc:
            logger.warning("sentiment history: redis read failed (%s)", exc)

    points = [e for e in _mem_history if e["observed_at"] >= cutoff]
    points.sort(key=lambda e: e["observed_at"])
    return points
