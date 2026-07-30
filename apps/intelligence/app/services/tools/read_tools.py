from typing import Any

import aiohttp
from pydantic import BaseModel

from ...config import settings
from ..guardrails import wrap_context_block
from ..vault_context import VaultContextFetcher
from .types import Tool, ToolContext

fetcher = VaultContextFetcher(
    api_base_url=settings.nester_api_base_url,
    service_api_key=settings.nester_service_api_key,
)

class EmptyArgs(BaseModel):
    pass

async def get_balance_handler(ctx: ToolContext, **kwargs: Any) -> Any:
    vaults = await fetcher.fetch_user_vaults(ctx.user_id)
    total = sum(v.get("balance_usd", 0) for v in vaults)
    return wrap_context_block("balance", f"Total balance: ${total:,.2f}")

get_balance = Tool(
    name="get_balance",
    description="Get the total balance of all user vaults.",
    args_model=EmptyArgs,
    consequential=False,
    handler=get_balance_handler,
)

async def get_portfolio_handler(ctx: ToolContext, **kwargs: Any) -> Any:
    vaults = await fetcher.fetch_user_vaults(ctx.user_id)
    market_rates = await fetcher.fetch_market_rates()
    block = fetcher.build_context_block(vaults, market_rates)
    return wrap_context_block("portfolio", block)

get_portfolio = Tool(
    name="get_portfolio",
    description="Get detailed portfolio information and current market rates.",
    args_model=EmptyArgs,
    consequential=False,
    handler=get_portfolio_handler,
)

async def get_market_rates_handler(ctx: ToolContext, **kwargs: Any) -> Any:
    market_rates = await fetcher.fetch_market_rates()
    block = fetcher.build_context_block([], market_rates)
    return wrap_context_block("market_rates", block)

get_market_rates = Tool(
    name="get_market_rates",
    description="Get current market rates for vaults.",
    args_model=EmptyArgs,
    consequential=False,
    handler=get_market_rates_handler,
)

async def get_risk_profile_handler(ctx: ToolContext, **kwargs: Any) -> Any:
    vaults = await fetcher.fetch_user_vaults(ctx.user_id)
    risk_data = {}
    for v in vaults:
        risk_data[v["id"]] = await fetcher.fetch_vault_risk(v["id"])
    block = fetcher.build_risk_profile_block(vaults, risk_data)
    return wrap_context_block("risk_profile", block)

get_risk_profile = Tool(
    name="get_risk_profile",
    description="Get risk profile assessment for the user's vaults.",
    args_model=EmptyArgs,
    consequential=False,
    handler=get_risk_profile_handler,
)

async def list_goals_handler(ctx: ToolContext, **kwargs: Any) -> Any:
    url = f"{settings.nester_api_base_url}/api/v1/users/savings-goals"
    # Read tools run automatically inside the chat loop, without a real
    # per-request user JWT (ctx.authorization_header is not populated
    # there) — so this authenticates the same way VaultContextFetcher's
    # calls are supposed to: the shared service key, scoped to this user
    # via X-User-Id. That header is required by the Go middleware's
    # service-auth branch (internal/middleware/auth.go) — don't drop it.
    headers = {
        "Authorization": f"Bearer {settings.nester_service_api_key}",
        "X-User-Id": ctx.user_id,
        "Content-Type": "application/json",
    }
    async with aiohttp.ClientSession() as session:
        async with session.get(url, headers=headers) as response:
            if response.status == 200:
                payload = await response.json()
                data = payload.get("data", payload) if isinstance(payload, dict) else payload
                return wrap_context_block("savings_goals", str(data))
            text = await response.text()
            return wrap_context_block(
                "savings_goals", f"Failed to fetch goals: {response.status} {text}"
            )

list_goals = Tool(
    name="list_goals",
    description="List all savings goals for the user.",
    args_model=EmptyArgs,
    consequential=False,
    handler=list_goals_handler,
)

READ_TOOLS = [get_balance, get_portfolio, get_market_rates, get_risk_profile, list_goals]
