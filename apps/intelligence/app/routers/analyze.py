"""Structured analysis endpoints — insights, sentiment, vault recommendations."""

from fastapi import APIRouter

from app.services.prometheus import (
    get_market_sentiment,
    get_portfolio_insights,
    get_vault_recommendations,
)

router = APIRouter()


@router.get("/portfolio/{user_id}/insights")
async def portfolio_insights(user_id: str) -> list[dict]:
    """Return AI-generated portfolio insight cards for a user."""
    return await get_portfolio_insights(user_id)


@router.get("/market/sentiment")
async def market_sentiment() -> dict:
    """Return current market sentiment for the Stellar DeFi / stablecoin space."""
    return await get_market_sentiment()


@router.get("/vaults/{vault_id}/recommendations")
async def vault_recommendations(vault_id: str) -> dict:
    """Return AI commentary and recommendations for a specific vault."""
    return await get_vault_recommendations(vault_id)
