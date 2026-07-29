import json
import logging
from typing import Any

import redis
from fastapi import APIRouter, Depends, HTTPException, Request
from pydantic import BaseModel

from ..config import settings
from ..dependencies.auth import verify_jwt
from ..services import guardrails
from ..services.conversation_store import store as conversation_store
from ..services.tool_audit_client import record_audit_event
from ..services.tools.registry import get_tool
from ..services.tools.types import ToolContext

logger = logging.getLogger(__name__)

router = APIRouter(tags=["tools"])


def get_redis_client() -> redis.Redis:
    if not settings.redis_url:
        raise HTTPException(status_code=500, detail="Redis is not configured")
    try:
        return redis.from_url(settings.redis_url, decode_responses=True)
    except Exception as e:
        logger.error(f"Redis connection failed: {e}")
        raise HTTPException(status_code=500, detail="Redis connection failed")


class ConfirmRequest(BaseModel):
    approved: bool


class ConfirmResponse(BaseModel):
    status: str
    assistant_message: str


@router.post("/tools/{proposal_id}/confirm", response_model=ConfirmResponse)
async def confirm_tool_action(
    proposal_id: str,
    payload: ConfirmRequest,
    request: Request,
    claims: dict[str, Any] = Depends(verify_jwt),
) -> ConfirmResponse:
    user_id = claims.get("sub")
    if not user_id:
        raise HTTPException(status_code=401, detail="Invalid user ID in token")

    r = get_redis_client()
    key = f"pending_action:{proposal_id}"
    data_str = r.get(key)
    if not data_str:
        raise HTTPException(status_code=404, detail="Proposal not found or expired")

    pending_action = json.loads(data_str)
    if pending_action.get("user_id") != user_id:
        raise HTTPException(status_code=403, detail="Not authorized for this proposal")

    tool_name = pending_action["tool_name"]
    try:
        tool = get_tool(tool_name)
    except KeyError:
        raise HTTPException(status_code=500, detail=f"Tool {tool_name} not found")

    if not tool.consequential:
        # A non-consequential tool should never have created a pending
        # action in the first place — reject rather than silently execute
        # something that was never gated in the first place.
        raise HTTPException(status_code=400, detail="This tool does not require confirmation")

    args = pending_action["arguments"]
    try:
        validated_args = tool.args_model(**args)
    except Exception as e:
        raise HTTPException(status_code=400, detail=f"Invalid arguments: {e}")

    r.delete(key)

    conversation_id = pending_action.get("conversation_id", "")
    request_id = pending_action.get("request_id") or proposal_id
    tool_use_id = pending_action.get("tool_use_id")

    def _append_tool_result(content: str, is_error: bool) -> None:
        # Every proposal turn stored an assistant message containing the
        # tool_use block (prometheus.py's stream_chat). The Anthropic API
        # requires that block to be followed by a matching tool_result
        # before the conversation can be replayed on a future turn — so
        # this must be a real tool_result, not placeholder text, or every
        # later chat turn for this user 400s on replay.
        if not tool_use_id:
            return
        block = {
            "type": "tool_result",
            "tool_use_id": tool_use_id,
            "content": content,
            "is_error": is_error,
        }
        conversation_store.append(user_id, "user", json.dumps([block]))

    if not payload.approved:
        await record_audit_event(
            user_id=user_id,
            request_id=request_id,
            conversation_id=conversation_id,
            tool_name=tool.name,
            arguments=args,
            consequential=True,
            status="declined",
        )
        assistant_message = "Okay, I won't do that."
        _append_tool_result("User declined this action.", is_error=False)
        conversation_store.append(user_id, "assistant", assistant_message)
        return ConfirmResponse(status="declined", assistant_message=assistant_message)

    await record_audit_event(
        user_id=user_id,
        request_id=request_id,
        conversation_id=conversation_id,
        tool_name=tool.name,
        arguments=args,
        consequential=True,
        status="confirmed",
    )

    # Execute using THIS request's own Authorization header — the user's
    # real, freshly-verified JWT — never the service-key bypass. Go's
    # handlers derive the acting user from that token themselves, so
    # ownership is enforced a second time, independently, on the Go side.
    ctx = ToolContext(
        user_id=user_id,
        request_id=request_id,
        conversation_id=conversation_id,
        authorization_header=request.headers.get("Authorization", ""),
    )

    try:
        result = await tool.handler(ctx, **validated_args.model_dump())
        status = "executed"
        error_message = ""
        assistant_message = "Done — that's been set up."
        _append_tool_result(
            guardrails.wrap_context_block(f"{tool.name}_result", json.dumps(result)),
            is_error=False,
        )
    except Exception as e:
        logger.error(f"Tool execution failed: {e}")
        result = None
        status = "failed"
        error_message = str(e)
        assistant_message = f"I wasn't able to complete that: {error_message}"
        _append_tool_result(error_message, is_error=True)

    await record_audit_event(
        user_id=user_id,
        request_id=request_id,
        conversation_id=conversation_id,
        tool_name=tool.name,
        arguments=args,
        consequential=True,
        status=status,
        result=result,
        error_message=error_message,
    )

    conversation_store.append(user_id, "assistant", assistant_message)

    return ConfirmResponse(status=status, assistant_message=assistant_message)
