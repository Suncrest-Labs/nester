"""Per-user conversation history with TTL eviction.

Uses Redis when INTELLIGENCE_REDIS_URL is configured so that all worker
instances share state and conversations survive restarts. Falls back to an
in-process dict when Redis is unavailable so dev and test environments need
no extra infrastructure.

Summarization support
---------------------
When the active history grows past the token threshold configured via
``INTELLIGENCE_MAX_HISTORY_TOKENS``, the active history is compacted (older
turns replaced by a Claude-generated summary) before the next assistant turn.

The full pre-summarization history is preserved at a separate Redis key
(``prometheus:conv:audit:<user_id>``) so it is always available for audit
purposes.  The audit key has a longer TTL (7 days) than the active key (24 h)
and is append-only — it is never trimmed or overwritten by the compaction.
"""

import json
import logging
from datetime import UTC, datetime, timedelta
from typing import Dict, List, Optional, Protocol, Union

from app.config import settings

logger = logging.getLogger(__name__)

_TTL_SECONDS = 86400          # 24 hours — active history
_AUDIT_TTL_SECONDS = 604800   # 7 days  — full audit history
_MAX_TURNS = 20               # hard cap for in-memory fallback
_KEY_PREFIX = "prometheus:conv:"
_AUDIT_KEY_PREFIX = "prometheus:conv:audit:"


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

    def _audit_key(self, user_id: str) -> str:
        return f"{_AUDIT_KEY_PREFIX}{user_id}"

    def get(self, user_id: str) -> List[Dict[str, str]]:
        if not self._available:
            return []

        try:
            raw: Optional[str] = self._client.get(self._key(user_id))  # type: ignore[assignment]
            if not raw:
                return []
            try:
                data = list(json.loads(raw))
                return data
            except Exception:
                return []
        except Exception as exc:
            logger.warning("Failed to read from Redis: %s", exc)
            self._available = False
            return []

    def append(self, user_id: str, role: str, content: str) -> None:
        if not self._available:
            return

        try:
            key = self._key(user_id)
            history = self.get(user_id)
            history.append({"role": role, "content": content})
            self._client.setex(key, _TTL_SECONDS, json.dumps(history))

            # Append to the audit log (full history, append-only, longer TTL).
            self._append_to_audit(user_id, role, content)
        except Exception as exc:
            logger.warning("Failed to write to Redis: %s", exc)
            self._available = False

    def _append_to_audit(self, user_id: str, role: str, content: str) -> None:
        """Append a single turn to the immutable audit history key."""
        if not self._available:
            return
        try:
            audit_key = self._audit_key(user_id)
            raw: Optional[str] = self._client.get(audit_key)  # type: ignore[assignment]
            audit: List[Dict[str, str]] = []
            if raw:
                try:
                    audit = list(json.loads(raw))
                except Exception:
                    audit = []
            audit.append({"role": role, "content": content})
            self._client.setex(audit_key, _AUDIT_TTL_SECONDS, json.dumps(audit))
        except Exception as exc:
            logger.warning("Failed to write to audit Redis key: %s", exc)

    def set_active(self, user_id: str, history: List[Dict[str, str]]) -> None:
        """Replace the active history (used after summarization compaction)."""
        if not self._available:
            return
        try:
            key = self._key(user_id)
            self._client.setex(key, _TTL_SECONDS, json.dumps(history))
        except Exception as exc:
            logger.warning("Failed to replace active history in Redis: %s", exc)
            self._available = False

    def get_audit(self, user_id: str) -> List[Dict[str, str]]:
        """Return the full audit history for ``user_id`` (all turns ever appended)."""
        if not self._available:
            return []
        try:
            raw: Optional[str] = self._client.get(self._audit_key(user_id))  # type: ignore[assignment]
            if not raw:
                return []
            return list(json.loads(raw))
        except Exception as exc:
            logger.warning("Failed to read audit history from Redis: %s", exc)
            return []

    def clear(self, user_id: str) -> None:
        if not self._available:
            return

        try:
            # Clear only the active key; the audit log is never deleted.
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
        # In-memory audit log (not persisted across restarts, but good for
        # dev/test environments).
        self._audit: Dict[str, List[Dict[str, str]]] = {}

    def get(self, user_id: str) -> List[Dict[str, str]]:
        self._evict_stale()
        return list(self._store.get(user_id, []))

    def append(self, user_id: str, role: str, content: str) -> None:
        self._evict_stale()
        if user_id not in self._store:
            self._store[user_id] = []
        self._store[user_id].append({"role": role, "content": content})
        self._touched[user_id] = datetime.now(UTC)

        # Audit log — append-only, no size cap.
        if user_id not in self._audit:
            self._audit[user_id] = []
        self._audit[user_id].append({"role": role, "content": content})

    def set_active(self, user_id: str, history: List[Dict[str, str]]) -> None:
        """Replace the active history (used after summarization compaction)."""
        self._store[user_id] = list(history)
        self._touched[user_id] = datetime.now(UTC)

    def get_audit(self, user_id: str) -> List[Dict[str, str]]:
        """Return the full audit history for ``user_id``."""
        return list(self._audit.get(user_id, []))

    def clear(self, user_id: str) -> None:
        # Clear active only; audit is never deleted.
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
    def set_active(self, user_id: str, history: List[Dict[str, str]]) -> None: ...
    def get_audit(self, user_id: str) -> List[Dict[str, str]]: ...
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
