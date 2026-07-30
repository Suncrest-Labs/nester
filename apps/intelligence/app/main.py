import logging
import re
import time
import uuid
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager

from fastapi import FastAPI, Request, Response
from slowapi import Limiter, _rate_limit_exceeded_handler
from slowapi.errors import RateLimitExceeded
from slowapi.util import get_remote_address

from app.middleware.request_id import RequestIDMiddleware, install_request_id_log_filter
from app.routers import (
    analyze,
    chat,
    coaching,
    deterioration,
    feedback,
    health,
    nudges,
    optimize,
    rebalance,
    recommendations,
    savings,
    tool_actions,
    ws_chat,
)


class RequestIDFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:  # noqa: A003
        record.request_id = getattr(record, "request_id", "")
        return super().format(record)


logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s [request_id=%(request_id)s]: %(message)s",
)
logger = logging.getLogger(__name__)
install_request_id_log_filter()

limiter = Limiter(key_func=get_remote_address)

# Bounded, safe charset for a client-supplied request ID (typical UUID/ULID/
# opaque-token shape). Anything else — oversized, containing control
# characters, etc. — is rejected in favour of a freshly minted UUID so an
# untrusted header can't smuggle log/header injection or unbounded values
# into request.state.request_id, response headers, or guardrail audit logs.
_REQUEST_ID_RE = re.compile(r"^[A-Za-z0-9._-]{1,128}$")


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncIterator[None]:
    logger.info("Intelligence service started")
    yield


app = FastAPI(title="Nester Intelligence", lifespan=lifespan)
app.state.limiter = limiter
app.add_middleware(RequestIDMiddleware)
app.add_exception_handler(RateLimitExceeded, _rate_limit_exceeded_handler)  # type: ignore[arg-type]


@app.middleware("http")
async def add_request_id(
    request: Request, call_next: Callable[[Request], Awaitable[Response]]
) -> Response:
    """Attach a request ID to every request for correlating guardrail logs.

    Reuses an inbound X-Request-Id if present and well-formed (e.g. from an
    upstream gateway), otherwise mints a fresh one, and always echoes it
    back. A malformed or oversized inbound value is replaced rather than
    trusted, since it flows into response headers and log lines.
    """
    inbound = request.headers.get("X-Request-Id", "")
    request_id = inbound if _REQUEST_ID_RE.match(inbound) else str(uuid.uuid4())
    request.state.request_id = request_id
    response = await call_next(request)
    response.headers["X-Request-Id"] = request_id
    return response


@app.middleware("http")
async def add_process_time_header(
    request: Request, call_next: Callable[[Request], Awaitable[Response]]
) -> Response:
    start_time = time.time()
    response = await call_next(request)
    process_time = time.time() - start_time
    logger.info(
        f"Endpoint: {request.url.path} | "
        f"Method: {request.method} | "
        f"Status: {response.status_code} | "
        f"Duration: {process_time:.4f}s | "
        f"RequestId: {getattr(request.state, 'request_id', '-')}"
    )
    return response


app.include_router(health.router)
app.include_router(chat.router, prefix="/intelligence")
app.include_router(coaching.router)
app.include_router(analyze.router)
app.include_router(ws_chat.router)
app.include_router(savings.router, prefix="/intelligence")
app.include_router(rebalance.router)
app.include_router(optimize.router, prefix="/intelligence")
app.include_router(recommendations.router, prefix="/intelligence")
app.include_router(nudges.router, prefix="/intelligence/nudges")
app.include_router(feedback.router, prefix="/intelligence")
app.include_router(tool_actions.router, prefix="/intelligence")
app.include_router(deterioration.router, prefix="/intelligence")
