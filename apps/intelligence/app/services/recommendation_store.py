"""Tracks dismissed and acted-on savings recommendations per user (#847).

Follows the exact pattern of `app.services.conversation_store`: a small
Protocol, a Redis-backed implementation with an in-memory fallback, TTL-based,
built via a module-level singleton so all requests in a worker share state.

Dismissed/acted-on candidates feed back into deterministic candidate
generation and ranking (see `recommendation_engine.py`) as retrieved context:
dismissed candidates are filtered out before the LLM ever sees them, and
action types the user has previously acted on get a deterministic priority
boost. None of this engagement history is handed to the LLM as an opaque
signal -- it only shapes which deterministic candidates are generated/ranked.
"""

import json
import logging
from datetime import UTC, datetime, timedelta
from typing import Dict, Literal, Optional, Protocol, Union

from app.config import settings

logger = logging.getLogger(__name__)

Engagement = Literal["dismissed", "acted_on"]

_TTL_SECONDS = 180 * 86400  # 180 days
_KEY_PREFIX = "prometheus:reco-engagement:"


# ---------------------------------------------------------------------------
# Redis-backed store
# ---------------------------------------------------------------------------


class _RedisEngagementStore:
    def __init__(self, redis_url: str) -> None:
        try:
            import redis as _redis

            self._client = _redis.from_url(redis_url, decode_responses=True)
            self._client.ping()
            logger.info("recommendation store: redis connected (%s)", redis_url)
            self._available = True
        except Exception as exc:
            logger.warning(
                "recommendation store: redis unavailable (%s), will fall back to in-memory",
                exc,
            )
            self._client = None  # type: ignore[assignment]
            self._available = False

    def _key(self, user_id: str) -> str:
        return f"{_KEY_PREFIX}{user_id}"

    def _read(self, user_id: str) -> Dict[str, Dict[str, str]]:
        if not self._available:
            return {}
        try:
            raw: Optional[str] = self._client.get(self._key(user_id))  # type: ignore[assignment]
            if not raw:
                return {}
            return dict(json.loads(raw))
        except Exception as exc:
            logger.warning("Failed to read engagement from Redis: %s", exc)
            self._available = False
            return {}

    def record(self, user_id: str, candidate_key: str, engagement: Engagement) -> None:
        if not self._available:
            return
        try:
            data = self._read(user_id)
            data[candidate_key] = {
                "engagement": engagement,
                "at": datetime.now(UTC).isoformat(),
            }
            self._client.setex(self._key(user_id), _TTL_SECONDS, json.dumps(data))
        except Exception as exc:
            logger.warning("Failed to write engagement to Redis: %s", exc)
            self._available = False

    def get_all(self, user_id: str) -> Dict[str, Dict[str, str]]:
        return self._read(user_id)

    def is_dismissed(self, user_id: str, candidate_key: str) -> bool:
        return self._read(user_id).get(candidate_key, {}).get("engagement") == "dismissed"


# ---------------------------------------------------------------------------
# In-memory fallback store
# ---------------------------------------------------------------------------


class _InMemoryEngagementStore:
    """Stores engagement keyed by user_id with TTL eviction."""

    def __init__(self, ttl_days: int = 180) -> None:
        self._ttl = timedelta(days=ttl_days)
        self._store: Dict[str, Dict[str, Dict[str, str]]] = {}
        self._touched: Dict[str, datetime] = {}

    def record(self, user_id: str, candidate_key: str, engagement: Engagement) -> None:
        self._evict_stale()
        self._store.setdefault(user_id, {})[candidate_key] = {
            "engagement": engagement,
            "at": datetime.now(UTC).isoformat(),
        }
        self._touched[user_id] = datetime.now(UTC)

    def get_all(self, user_id: str) -> Dict[str, Dict[str, str]]:
        self._evict_stale()
        # Copy nested dicts too so a caller mutating the result can't corrupt
        # the store's internal state.
        return {key: dict(value) for key, value in self._store.get(user_id, {}).items()}

    def is_dismissed(self, user_id: str, candidate_key: str) -> bool:
        return self.get_all(user_id).get(candidate_key, {}).get("engagement") == "dismissed"

    def _evict_stale(self) -> None:
        cutoff = datetime.now(UTC) - self._ttl
        stale = [uid for uid, t in self._touched.items() if t < cutoff]
        for uid in stale:
            self._store.pop(uid, None)
            self._touched.pop(uid, None)


# ---------------------------------------------------------------------------
# Protocol type for type checking
# ---------------------------------------------------------------------------


class EngagementStore(Protocol):
    def record(self, user_id: str, candidate_key: str, engagement: Engagement) -> None: ...
    def get_all(self, user_id: str) -> Dict[str, Dict[str, str]]: ...
    def is_dismissed(self, user_id: str, candidate_key: str) -> bool: ...


# ---------------------------------------------------------------------------
# Module-level singleton -- shared across all requests in this worker
# ---------------------------------------------------------------------------


def _build_store() -> Union[_RedisEngagementStore, _InMemoryEngagementStore]:
    redis_url = settings.redis_url
    if redis_url:
        try:
            s = _RedisEngagementStore(redis_url)
            if s._available:
                logger.info("recommendation store: redis (%s)", redis_url)
                return s
            logger.warning(
                "recommendation store: redis connection failed, using in-memory fallback"
            )
        except Exception as exc:
            logger.warning(
                "recommendation store: redis unavailable (%s), using in-memory fallback", exc
            )
    else:
        logger.info("recommendation store: in-memory (single-instance only)")
    return _InMemoryEngagementStore()


store: Union[_RedisEngagementStore, _InMemoryEngagementStore] = _build_store()
