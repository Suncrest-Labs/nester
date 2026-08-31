"""Prometheus exposition endpoint for the intelligence service (nester#1056).

The Go API keeps its exposition on a separate loopback listener, which is the
stronger arrangement: there is no rule ordering to get wrong. This service
serves a single port, so the route is guarded by a shared bearer token
instead. Exposition is not public data — it reveals internal route names,
traffic volumes, and error rates, which together map the service and signal
when it is degraded.
"""

from __future__ import annotations

import hmac

from fastapi import APIRouter, HTTPException, Request, Response, status
from prometheus_client import CONTENT_TYPE_LATEST, generate_latest

from app import metrics
from app.config import settings

router = APIRouter()


def _authorized(request: Request) -> bool:
    """Whether the caller may scrape.

    An empty configured token disables the check, for deployments where the
    port is not publicly routable. That is a deliberate opt-out rather than a
    default: ``metrics_token`` unset in an environment that exposes the port
    is a misconfiguration the deployment documentation calls out.
    """
    expected = settings.metrics_token
    if not expected:
        return True

    header = request.headers.get("Authorization", "")
    scheme, _, presented = header.partition(" ")
    if scheme.lower() != "bearer":
        return False

    # Constant-time comparison: a token checked with == leaks its prefix
    # through response timing, and a scrape endpoint is reachable often enough
    # for that to be worth exploiting.
    return hmac.compare_digest(presented, expected)


@router.get("/metrics", include_in_schema=False)
async def scrape(request: Request) -> Response:
    if not settings.metrics_enabled:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND)

    if not _authorized(request):
        # 404 rather than 401: an unauthenticated prober learns nothing about
        # whether the endpoint exists.
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND)

    return Response(
        content=generate_latest(metrics.REGISTRY),
        media_type=CONTENT_TYPE_LATEST,
    )
