"""Structured analysis endpoints — insights, sentiment, vault recommendations."""

from typing import Any

from fastapi import APIRouter, Depends, HTTPException, status

from app.dependencies.auth import verify_jwt
from app.services.prometheus import (
    get_market_sentiment,
    get_portfolio_insights,
    get_vault_recommendations,
)

router = APIRouter(dependencies=[Depends(verify_jwt)])


@router.get("/portfolio/{user_id}/insights")
async def portfolio_insights(
    user_id: str,
    claims: dict[str, Any] = Depends(verify_jwt),
) -> list[dict[str, Any]]:
    """Return AI-generated portfolio insight cards for a user.

    The path ``user_id`` must match the authenticated subject to prevent
    one user querying another's insights.
    """
    if claims.get("sub") != user_id:
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN,
            detail="You are not authorised to access this user's insights",
        )
    return await get_portfolio_insights(user_id)


@router.get("/market/sentiment")
async def market_sentiment() -> dict[str, Any]:
    """Return current market sentiment for the Stellar DeFi / stablecoin space."""
    return await get_market_sentiment()


@router.get("/vaults/{vault_id}/recommendations")
async def vault_recommendations(vault_id: str) -> dict[str, Any]:
    """Return AI commentary and recommendations for a specific vault."""
    return await get_vault_recommendations(vault_id)
