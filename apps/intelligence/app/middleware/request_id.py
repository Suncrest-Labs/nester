"""Request-ID correlation middleware for the intelligence service.

Every inbound request is assigned a correlation ID that is:
- Taken from the ``X-Request-ID`` header if the upstream Go API forwarded one.
- Generated as a UUIDv4 if no header is present (e.g. direct calls in tests).

The ID is:
1. Bound to a ``contextvars.ContextVar`` so any coroutine in the call-chain
   can read it without threading it through every function signature.
2. Added to every structured log line emitted during the request via a custom
   ``logging.Filter``.
3. Echoed back to the caller as the ``X-Request-ID`` response header so
   clients can correlate a failed response to server-side logs.
"""

import logging
import uuid
from collections.abc import Awaitable, Callable

# ---------------------------------------------------------------------------
# Context variable — readable anywhere inside the async call-chain.
# ---------------------------------------------------------------------------
from contextvars import ContextVar

from fastapi import Request, Response
from starlette.middleware.base import BaseHTTPMiddleware

request_id_var: ContextVar[str] = ContextVar("request_id", default="")

HEADER_NAME = "X-Request-ID"


# ---------------------------------------------------------------------------
# Logging filter — injects the current request ID into every log record.
# ---------------------------------------------------------------------------


class _RequestIDFilter(logging.Filter):
    """Attach the current ``request_id`` to every ``LogRecord``."""

    def filter(self, record: logging.LogRecord) -> bool:  # noqa: A003
        record.request_id = request_id_var.get("")
        return True


def install_request_id_log_filter() -> None:
    """Attach the filter to the root logger once at startup.

    Call this once from the application lifespan or module-level setup so
    that all loggers in the process inherit the filter automatically.
    """
    root = logging.getLogger()
    for handler in root.handlers:
        handler.addFilter(_RequestIDFilter())


# ---------------------------------------------------------------------------
# ASGI / Starlette middleware
# ---------------------------------------------------------------------------


class RequestIDMiddleware(BaseHTTPMiddleware):
    """Read or generate a correlation ID and propagate it through the request.

    - Reads ``X-Request-ID`` from the inbound request headers.
    - Falls back to a newly generated UUIDv4 when the header is absent.
    - Stores the ID in the ``request_id_var`` ContextVar for the duration of
      the request so downstream code can call ``request_id_var.get()``.
    - Sets ``X-Request-ID`` on the outgoing response so the caller receives
      the same ID that was used server-side.
    """

    async def dispatch(
        self,
        request: Request,
        call_next: Callable[[Request], Awaitable[Response]],
    ) -> Response:
        rid = request.headers.get(HEADER_NAME) or str(uuid.uuid4())

        token = request_id_var.set(rid)
        try:
            response = await call_next(request)
        finally:
            request_id_var.reset(token)

        response.headers[HEADER_NAME] = rid
        return response
