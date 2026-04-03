"""In-memory per-user conversation history with TTL eviction."""

from datetime import UTC, datetime, timedelta


class ConversationStore:
    """Stores chat history keyed by user_id.

    Each entry is a list of Anthropic-format message dicts
    ({"role": "user"|"assistant", "content": str}).

    Entries are evicted after `ttl_minutes` of inactivity.
    Call `evict_stale()` periodically (e.g. in a background task)
    or it will be called lazily on every `get` / `append`.
    """

    def __init__(self, ttl_minutes: int = 60, max_turns: int = 20) -> None:
        self._ttl = timedelta(minutes=ttl_minutes)
        self._max_turns = max_turns  # keep last N messages to cap token spend
        self._store: dict[str, list[dict]] = {}
        self._touched: dict[str, datetime] = {}

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def get(self, user_id: str) -> list[dict]:
        self._evict_stale()
        return list(self._store.get(user_id, []))

    def append(self, user_id: str, role: str, content: str) -> None:
        if user_id not in self._store:
            self._store[user_id] = []
        self._store[user_id].append({"role": role, "content": content})
        # Trim to last max_turns messages (preserve pairs: user+assistant)
        if len(self._store[user_id]) > self._max_turns:
            self._store[user_id] = self._store[user_id][-self._max_turns :]
        self._touched[user_id] = datetime.now(UTC)

    def clear(self, user_id: str) -> None:
        self._store.pop(user_id, None)
        self._touched.pop(user_id, None)

    # ------------------------------------------------------------------
    # Internal
    # ------------------------------------------------------------------

    def _evict_stale(self) -> None:
        cutoff = datetime.now(UTC) - self._ttl
        stale = [uid for uid, t in self._touched.items() if t < cutoff]
        for uid in stale:
            self._store.pop(uid, None)
            self._touched.pop(uid, None)


# Module-level singleton shared across requests
store = ConversationStore(ttl_minutes=60, max_turns=20)
