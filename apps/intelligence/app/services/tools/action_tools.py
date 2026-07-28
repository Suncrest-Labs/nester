from datetime import datetime, timezone
from typing import Any, Optional

import aiohttp
from pydantic import BaseModel, ConfigDict

from ...config import settings
from .types import Tool, ToolContext


class CreateSavingsGoalArgs(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    target_amount: float
    currency: str
    deadline: datetime
    category: Optional[str] = None
    emoji: Optional[str] = None


async def create_savings_goal_handler(ctx: ToolContext, **kwargs: Any) -> Any:
    deadline = kwargs.pop("deadline")
    if isinstance(deadline, datetime):
        if deadline.tzinfo is None:
            deadline = deadline.replace(tzinfo=timezone.utc)
        deadline = deadline.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    body = {**kwargs, "deadline": deadline}

    url = f"{settings.nester_api_base_url}/api/v1/users/savings-goals"
    headers = {
        "Authorization": ctx.authorization_header,
        "Content-Type": "application/json",
    }
    async with aiohttp.ClientSession() as session:
        async with session.post(url, headers=headers, json=body) as response:
            if response.status in (200, 201):
                return await response.json()
            text = await response.text()
            raise Exception(f"Failed to create savings goal: {response.status} {text}")


def create_savings_goal_template(args: dict[str, Any]) -> str:
    return (
        f"Create a savings goal '{args.get('name')}' for {args.get('target_amount')} "
        f"{args.get('currency')} by {args.get('deadline')}?"
    )


create_savings_goal = Tool(
    name="create_savings_goal",
    description="Create a new savings goal.",
    args_model=CreateSavingsGoalArgs,
    consequential=True,
    handler=create_savings_goal_handler,
    confirmation_template=create_savings_goal_template,
)


class CreateRecurringDepositArgs(BaseModel):
    model_config = ConfigDict(extra="forbid")

    goal_id: str
    amount: float
    currency: str
    frequency: str


async def _fetch_goal_vault_id(
    session: aiohttp.ClientSession, headers: dict[str, str], goal_id: str
) -> Optional[str]:
    url = f"{settings.nester_api_base_url}/api/v1/users/savings-goals/{goal_id}"
    async with session.get(url, headers=headers) as response:
        if response.status != 200:
            text = await response.text()
            raise Exception(f"Failed to look up goal {goal_id}: {response.status} {text}")
        payload = await response.json()
        # Go's response envelope is {"success": true, "data": {...}}; tolerate
        # either that shape or a bare goal object.
        goal = payload.get("data", payload) if isinstance(payload, dict) else payload
        vault_id = goal.get("vault_id")
        return str(vault_id) if vault_id else None


async def create_recurring_deposit_handler(ctx: ToolContext, **kwargs: Any) -> Any:
    goal_id = kwargs.pop("goal_id")
    headers = {
        "Authorization": ctx.authorization_header,
        "Content-Type": "application/json",
    }
    async with aiohttp.ClientSession() as session:
        vault_id = await _fetch_goal_vault_id(session, headers, goal_id)
        if not vault_id:
            raise Exception(
                "This goal isn't linked to a vault yet. Link a vault to the goal before "
                "setting up a recurring deposit."
            )

        url = f"{settings.nester_api_base_url}/api/v1/users/savings-goals/{goal_id}/schedule"
        body = {**kwargs, "vault_id": vault_id}
        async with session.post(url, headers=headers, json=body) as response:
            if response.status in (200, 201):
                return await response.json()
            text = await response.text()
            raise Exception(f"Failed to create recurring deposit: {response.status} {text}")


def create_recurring_deposit_template(args: dict[str, Any]) -> str:
    return (
        f"Set up a recurring deposit of {args.get('amount')} {args.get('currency')} "
        f"every {args.get('frequency')} into this goal?"
    )


create_recurring_deposit = Tool(
    name="create_recurring_deposit",
    description="Create a recurring deposit schedule for an existing savings goal.",
    args_model=CreateRecurringDepositArgs,
    consequential=True,
    handler=create_recurring_deposit_handler,
    confirmation_template=create_recurring_deposit_template,
)

ACTION_TOOLS = [create_savings_goal, create_recurring_deposit]
