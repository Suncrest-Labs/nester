"""Client for writing tool-invocation events to the Go tamper-evident audit log.

Every proposed/rejected/confirmed/declined/executed/failed transition for a
tool call goes through here. Writes are best-effort: an audit-log outage must
never block the chat or confirmation flow, but every failure is logged so
gaps are visible in service logs rather than silently swallowed.
"""

import logging
from typing import Any

import aiohttp

from ..config import settings

logger = logging.getLogger(__name__)


async def record_audit_event(
    *,
    user_id: str,
    request_id: str,
    conversation_id: str,
    tool_name: str,
    arguments: Any,
    consequential: bool,
    status: str,
    result: Any = None,
    error_message: str = "",
) -> None:
    url = f"{settings.nester_api_base_url}/api/v1/internal/intelligence/tool-audit"
    payload = {
        "user_id": user_id,
        "request_id": request_id,
        "conversation_id": conversation_id,
        "tool_name": tool_name,
        "arguments": arguments,
        "consequential": consequential,
        "status": status,
        "result": result,
        "error_message": error_message,
    }
    try:
        async with aiohttp.ClientSession(timeout=aiohttp.ClientTimeout(total=5)) as session:
            async with session.post(
                url,
                json=payload,
                headers={
                    "Authorization": f"Bearer {settings.nester_service_api_key}",
                    "X-User-Id": user_id,
                },
            ) as response:
                if response.status >= 400:
                    text = await response.text()
                    logger.warning(
                        "tool-audit write failed tool=%s status=%s http_status=%s body=%s",
                        tool_name, status, response.status, text,
                    )
    except Exception:
        logger.exception(
            "tool-audit write raised tool=%s status=%s", tool_name, status
        )
