"""Streaming SSE chat endpoint for Prometheus AI."""

from typing import Any

from fastapi import APIRouter, Depends, HTTPException, Query, Request, status
from fastapi.responses import StreamingResponse
from slowapi import Limiter
from slowapi.util import get_remote_address

from app.dependencies.auth import verify_jwt
from app.services.conversation_store import store as conversation_store
from app.services.prometheus import stream_chat

router = APIRouter(dependencies=[Depends(verify_jwt)])

_limiter = Limiter(key_func=get_remote_address)


@router.get("/chat")
@_limiter.limit("30/minute")
async def chat(
    request: Request,
    message: str = Query(..., description="User message to Prometheus"),
    language: str | None = Query(
        None, description="Preferred response language (ISO 639-1, e.g. 'fr', 'sw')"
    ),
    claims: dict[str, Any] = Depends(verify_jwt),
) -> StreamingResponse:
    """Stream a Prometheus AI response as Server-Sent Events.

    The user ID is sourced from the JWT subject claim — never from the caller.
    Each event is ``data: <text chunk>\\n\\n``.
    The stream terminates with ``data: [DONE]\\n\\n``.

    `language` is the user's stored language preference (shared with the
    frontend i18n settings, #789); when omitted, Prometheus detects the
    language of `message` as a fallback (#multilingual).
    """
    user_id: str = claims.get("sub", "")
    if not user_id:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Token missing subject claim",
        )
    return StreamingResponse(
        stream_chat(user_id, message, language),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "X-Accel-Buffering": "no",
        },
    )


@router.delete("/conversation", status_code=status.HTTP_200_OK)
async def clear_conversation(
    claims: dict[str, Any] = Depends(verify_jwt),
) -> dict[str, str]:
    """Clear the authenticated user's conversation history.

    The user ID is sourced from the JWT subject claim — never from the caller.
    """
    user_id: str = claims.get("sub", "")
    if not user_id:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Token missing subject claim",
        )
    conversation_store.clear(user_id)
    return {"message": "Conversation history cleared"}

