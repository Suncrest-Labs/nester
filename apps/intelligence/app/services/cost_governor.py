import logging
from datetime import UTC, datetime
from typing import Any

from app.config import settings

logger = logging.getLogger(__name__)

class CostGovernor:
    def __init__(self, redis_url: str):
        self._client: Any = None
        try:
            import redis
            self._client = redis.from_url(redis_url, decode_responses=True)
            self._client.ping()
            self._available = True
        except Exception as exc:
            logger.warning("CostGovernor: redis unavailable (%s), falling back to NOOP", exc)
            self._client = None
            self._available = False

    def _key(self, user_id: str) -> str:
        date_str = datetime.now(UTC).strftime("%Y-%m-%d")
        return f"prometheus:usage:{date_str}:{user_id}"

    def check_budget(self, user_id: str) -> bool:
        if not self._available:
            return True
        try:
            val = self._client.get(self._key(user_id))
            used = int(val) if val else 0
            return used < settings.daily_token_budget_per_user
        except Exception as e:
            logger.warning(f"CostGovernor: failed to check budget: {e}")
            self._available = False
            return True

    def record_usage(self, user_id: str, tokens: int) -> None:
        if not self._available:
            return
        try:
            key = self._key(user_id)
            current = self._client.incrby(key, tokens)
            if current == tokens:
                # First write today, set TTL to 48 hours to be safe
                self._client.expire(key, 172800)
        except Exception as e:
            logger.warning(f"CostGovernor: failed to record usage: {e}")
            self._available = False

cost_governor = CostGovernor(settings.redis_url)
