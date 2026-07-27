"""Personalized savings recommendation endpoints (#847)."""

import re
from typing import Any

from fastapi import APIRouter, Depends, HTTPException, Query, Request, status
from slowapi import Limiter
from slowapi.util import get_remote_address

from app.dependencies.auth import verify_jwt
from app.models.savings_recommendation import SavingsRecommendationSet
from app.services.recommendation_engine import engine

router = APIRouter(dependencies=[Depends(verify_jwt)])

_limiter = Limiter(key_func=get_remote_address)

# candidate_id is our own composite key (e.g. "increase_contribution:goal-123"),
# not a UUID/contract-id -- validate charset and length instead.
_CANDIDATE_ID_RE = re.compile(r"^[A-Za-z0-9_:+-]{1,160}$")


def _validate_candidate_id(value: str) -> str:
    if _CANDIDATE_ID_RE.match(value):
        return value
    raise HTTPException(
        status_code=status.HTTP_400_BAD_REQUEST, detail="Invalid candidate id format"
    )


def _require_user_id(claims: dict[str, Any]) -> str:
    user_id = claims.get("sub", "")
    if not user_id:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED, detail="User ID not found in token"
        )
    return str(user_id)


@router.get("/savings-recommendations", response_model=SavingsRecommendationSet)
@_limiter.limit("10/minute")
async def get_savings_recommendations(
    request: Request,
    risk_tolerance: str = Query(default="moderate"),
    refresh: bool = Query(default=False),
    claims: dict[str, Any] = Depends(verify_jwt),
) -> SavingsRecommendationSet:
    """Return the authenticated user's personalized savings recommendations.

    Candidate actions (increase contribution, move to higher yield, lock for
    a term boost, consolidate goals) are computed deterministically from the
    user's own data; Claude only selects, prioritizes, and explains them.

    Cached on a cadence and regenerated automatically when the user's
    goals/vault balances change materially -- not on every page load. Pass
    `refresh=true` to force regeneration.
    """
    user_id = _require_user_id(claims)
    if risk_tolerance not in ("conservative", "moderate", "aggressive"):
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST, detail="Invalid risk_tolerance"
        )
    return await engine.generate_for_user(user_id, risk_tolerance, force_refresh=refresh)


@router.post("/savings-recommendations/{candidate_id}/dismiss")
@_limiter.limit("30/minute")
async def dismiss_recommendation(
    request: Request,
    candidate_id: str,
    claims: dict[str, Any] = Depends(verify_jwt),
) -> dict[str, bool]:
    """Mark a recommendation dismissed so it is never re-recommended."""
    user_id = _require_user_id(claims)
    safe_id = _validate_candidate_id(candidate_id)
    engine.dismiss(user_id, safe_id)
    return {"ok": True}


@router.post("/savings-recommendations/{candidate_id}/acted-on")
@_limiter.limit("30/minute")
async def mark_recommendation_acted_on(
    request: Request,
    candidate_id: str,
    claims: dict[str, Any] = Depends(verify_jwt),
) -> dict[str, bool]:
    """Mark a recommendation as acted on. Future candidates of the same
    action type are weighted higher for this user."""
    user_id = _require_user_id(claims)
    safe_id = _validate_candidate_id(candidate_id)
    engine.mark_acted_on(user_id, safe_id)
    return {"ok": True}
