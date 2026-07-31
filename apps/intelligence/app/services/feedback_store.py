"""Per-user conversation feedback (thumbs-up/down) with TTL eviction.

Follows the exact pattern of `app.services.recommendation_store` and
`app.services.conversation_store`: a small Protocol, a Redis-backed
implementation with an in-memory fallback, TTL-based, built via a
module-level singleton so all requests in a worker share state.

Stored feedback entries include the rating, an optional free-text comment,
and a reference to the conversation turn. The data is intended for tracking
response quality over time and feeding into evaluation datasets.
"""

import json
import logging
from datetime import UTC, datetime, timedelta
from typing import Dict, List, Literal, Optional, Protocol, TypedDict, Union

from app.config import settings

logger = logging.getLogger(__name__)

FeedbackRating = Literal["thumbs_up", "thumbs_down"]

_TTL_SECONDS = 365 * 86400  # 365 days — feedback is valuable for eval datasets
_KEY_PREFIX = "prometheus:feedback:"
_MAX_FEEDBACK_PER_USER = 500  # cap to bound storage


class FeedbackEntryDict(TypedDict):
    """Shape of a stored feedback entry."""
    id: str
    rating: FeedbackRating
    comment: str
    conversation_id: str
    user_id: str
    created_at: str


# ---------------------------------------------------------------------------
# Redis-backed store
# ---------------------------------------------------------------------------


class _RedisFeedbackStore:
    def __init__(self, redis_url: str) -> None:
        try:
            import redis as _redis

            self._client = _redis.from_url(redis_url, decode_responses=True)
            self._client.ping()
            logger.info("feedback store: redis connected (%s)", redis_url)
            self._available = True
        except Exception as exc:
            logger.warning(
                "feedback store: redis unavailable (%s), will fall back to in-memory",
                exc,
            )
            self._client = None  # type: ignore[assignment]
            self._available = False

    def _key(self, user_id: str) -> str:
        return f"{_KEY_PREFIX}{user_id}"

    def _read(self, user_id: str) -> List[FeedbackEntryDict]:
        if not self._available:
            return []
        try:
            raw: Optional[str] = self._client.get(self._key(user_id))  # type: ignore[assignment]
            if not raw:
                return []
            data = list(json.loads(raw))
            return [FeedbackEntryDict(**item) for item in data]
        except Exception as exc:
            logger.warning("Failed to read feedback from Redis: %s", exc)
            self._available = False
            return []

    def submit(
        self,
        user_id: str,
        entry: FeedbackEntryDict,
    ) -> None:
        if not self._available:
            return
        try:
            history = self._read(user_id)
            history.append(entry)
            # Cap to bound storage
            if len(history) > _MAX_FEEDBACK_PER_USER:
                history = history[-_MAX_FEEDBACK_PER_USER:]
            self._client.setex(self._key(user_id), _TTL_SECONDS, json.dumps(history))
        except Exception as exc:
            logger.warning("Failed to write feedback to Redis: %s", exc)
            self._available = False

    def get_all(self, user_id: str) -> List[FeedbackEntryDict]:
        return self._read(user_id)


# ---------------------------------------------------------------------------
# In-memory fallback store
# ---------------------------------------------------------------------------


class _InMemoryFeedbackStore:
    """Stores feedback keyed by user_id with TTL eviction."""

    def __init__(self, ttl_days: int = 365) -> None:
        self._ttl = timedelta(days=ttl_days)
        self._store: Dict[str, List[FeedbackEntryDict]] = {}
        self._touched: Dict[str, datetime] = {}

    def submit(
        self,
        user_id: str,
        entry: FeedbackEntryDict,
    ) -> None:
        self._evict_stale()
        if user_id not in self._store:
            self._store[user_id] = []
        self._store[user_id].append(entry)
        # Cap to bound storage
        if len(self._store[user_id]) > _MAX_FEEDBACK_PER_USER:
            self._store[user_id] = self._store[user_id][-_MAX_FEEDBACK_PER_USER:]
        self._touched[user_id] = datetime.now(UTC)

    def get_all(self, user_id: str) -> List[FeedbackEntryDict]:
        self._evict_stale()
        # Return a deep-ish copy so callers can't mutate internal state
        return [FeedbackEntryDict(**entry) for entry in self._store.get(user_id, [])]

    def _evict_stale(self) -> None:
        cutoff = datetime.now(UTC) - self._ttl
        stale = [uid for uid, t in self._touched.items() if t < cutoff]
        for uid in stale:
            self._store.pop(uid, None)
            self._touched.pop(uid, None)


# ---------------------------------------------------------------------------
# Protocol type for type checking
# ---------------------------------------------------------------------------


class FeedbackStore(Protocol):
    def submit(
        self,
        user_id: str,
        entry: FeedbackEntryDict,
    ) -> None: ...

    def get_all(self, user_id: str) -> List[FeedbackEntryDict]: ...


# ---------------------------------------------------------------------------
# Module-level singleton — shared across all requests in this worker
# ---------------------------------------------------------------------------


def _build_store() -> Union[_RedisFeedbackStore, _InMemoryFeedbackStore]:
    redis_url = settings.redis_url
    if redis_url:
        try:
            s = _RedisFeedbackStore(redis_url)
            if s._available:
                logger.info("feedback store: redis (%s)", redis_url)
                return s
            logger.warning(
                "feedback store: redis connection failed, using in-memory fallback"
            )
        except Exception as exc:
            logger.warning(
                "feedback store: redis unavailable (%s), using in-memory fallback", exc
            )
    else:
        logger.info("feedback store: in-memory (single-instance only)")
    return _InMemoryFeedbackStore()


store: Union[_RedisFeedbackStore, _InMemoryFeedbackStore] = _build_store()
