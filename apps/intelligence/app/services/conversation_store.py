"""Per-user conversation history with TTL eviction.

Uses Redis when INTELLIGENCE_REDIS_URL is configured so that all worker
instances share state and conversations survive restarts. Falls back to an
in-process dict when Redis is unavailable so dev and test environments need
no extra infrastructure.
"""

import json
import logging
from datetime import UTC, datetime, timedelta
from typing import Dict, List, Optional, Protocol, Union

from app.config import settings

logger = logging.getLogger(__name__)

_TTL_SECONDS = 86400  # 24 hours of inactivity
_MAX_TURNS = 20       # keep the last N messages to cap token spend
_KEY_PREFIX = "prometheus:conv:"


# ---------------------------------------------------------------------------
# Redis-backed store
# ---------------------------------------------------------------------------

class _RedisConversationStore:
    def __init__(self, redis_url: str) -> None:
        try:
            import redis as _redis
            self._client = _redis.from_url(redis_url, decode_responses=True)
            # Test connection
            self._client.ping()
            logger.info("conversation store: redis connected (%s)", redis_url)
            self._available = True
        except Exception as exc:
            logger.warning(
                "conversation store: redis unavailable (%s), will fall back to in-memory", exc
            )
            self._client = None  # type: ignore[assignment]
            self._available = False

    def _key(self, user_id: str) -> str:
        return f"{_KEY_PREFIX}{user_id}"

    def get(self, user_id: str) -> List[Dict[str, str]]:
        if not self._available:
            # Fallback to empty list if Redis not available
            return []

        try:
            raw: Optional[str] = self._client.get(self._key(user_id))  # type: ignore[assignment]
            if not raw:
                return []
            try:
                data = list(json.loads(raw))
                # Trim to last 20 messages as required
                return data[-_MAX_TURNS:] if len(data) > _MAX_TURNS else data
            except Exception:
                return []
        except Exception as exc:
            logger.warning("Failed to read from Redis: %s", exc)
            # Mark as unavailable for subsequent calls
            self._available = False
            return []

    def append(self, user_id: str, role: str, content: str) -> None:
        if not self._available:
            # Silently fail if Redis not available
            return

        try:
            key = self._key(user_id)
            history = self.get(user_id)  # This will handle trimming
            history.append({"role": role, "content": content})
            # Reset TTL on write
            self._client.setex(key, _TTL_SECONDS, json.dumps(history))
        except Exception as exc:
            logger.warning("Failed to write to Redis: %s", exc)
            # Mark as unavailable for subsequent calls
            self._available = False

    def clear(self, user_id: str) -> None:
        if not self._available:
            return

        try:
            self._client.delete(self._key(user_id))
        except Exception as exc:
            logger.warning("Failed to clear Redis key: %s", exc)
            self._available = False


# ---------------------------------------------------------------------------
# In-memory fallback store
# ---------------------------------------------------------------------------

class _InMemoryConversationStore:
    """Stores chat history keyed by user_id with TTL eviction."""

    def __init__(self, ttl_minutes: int = 1440, max_turns: int = 20) -> None:
        self._ttl = timedelta(minutes=ttl_minutes)
        self._max_turns = max_turns
        self._store: Dict[str, List[Dict[str, str]]] = {}
        self._touched: Dict[str, datetime] = {}

    def get(self, user_id: str) -> List[Dict[str, str]]:
        self._evict_stale()
        history = self._store.get(user_id, [])
        # Trim to last 20 messages as required
        return history[-_MAX_TURNS:] if len(history) > _MAX_TURNS else history

    def append(self, user_id: str, role: str, content: str) -> None:
        self._evict_stale()
        if user_id not in self._store:
            self._store[user_id] = []
        self._store[user_id].append({"role": role, "content": content})
        if len(self._store[user_id]) > self._max_turns:
            self._store[user_id] = self._store[user_id][-self._max_turns:]
        self._touched[user_id] = datetime.now(UTC)

    def clear(self, user_id: str) -> None:
        self._store.pop(user_id, None)
        self._touched.pop(user_id, None)

    def _evict_stale(self) -> None:
        cutoff = datetime.now(UTC) - self._ttl
        stale = [uid for uid, t in self._touched.items() if t < cutoff]
        for uid in stale:
            self._store.pop(uid, None)
            self._touched.pop(uid, None)


# ---------------------------------------------------------------------------
# Protocol type for type checking
# ---------------------------------------------------------------------------

class ConversationStore(Protocol):
    def get(self, user_id: str) -> List[Dict[str, str]]: ...
    def append(self, user_id: str, role: str, content: str) -> None: ...
    def clear(self, user_id: str) -> None: ...


# ---------------------------------------------------------------------------
# Module-level singleton — shared across all requests in this worker
# ---------------------------------------------------------------------------

def _build_store() -> Union[_RedisConversationStore, _InMemoryConversationStore]:
    redis_url = settings.redis_url
    if redis_url:
        try:
            s = _RedisConversationStore(redis_url)
            if s._available:  # Only return Redis store if connection worked
                logger.info("conversation store: redis (%s)", redis_url)
                return s
            else:
                logger.warning(
                    "conversation store: redis connection failed, using in-memory fallback"
                )
        except Exception as exc:
            logger.warning(
                "conversation store: redis unavailable (%s), using in-memory fallback", exc
            )
    else:
        logger.info(
            "conversation store: in-memory (single-instance only; "
            "set INTELLIGENCE_REDIS_URL for production)"
        )
    return _InMemoryConversationStore(ttl_minutes=1440, max_turns=_MAX_TURNS)  # 24 hours TTL


store: Union[_RedisConversationStore, _InMemoryConversationStore] = _build_store()
