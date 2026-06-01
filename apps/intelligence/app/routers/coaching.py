"""Savings coaching endpoint — deposit schedule and progress assessment."""

from typing import Any

from fastapi import APIRouter, Depends, Request
from slowapi import Limiter
from slowapi.util import get_remote_address

from app.dependencies.auth import verify_jwt
from app.models.coaching import CoachingRequest, CoachingResponse
from app.services.prometheus import generate_coaching

router = APIRouter(prefix="/intelligence", dependencies=[Depends(verify_jwt)])

_limiter = Limiter(key_func=get_remote_address)


@router.post("/coaching", response_model=CoachingResponse)
@_limiter.limit("20/minute")
async def coaching(
    request: Request,
    body: CoachingRequest,
    claims: dict[str, Any] = Depends(verify_jwt),  # noqa: ARG001
) -> CoachingResponse:
    """Return a Prometheus-generated deposit schedule and progress assessment."""
    result: CoachingResponse = await generate_coaching(body)
    return result
