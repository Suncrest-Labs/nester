"""Streaming SSE chat endpoint for Prometheus AI."""

from typing import Any

from fastapi import APIRouter, Depends, HTTPException, Query, status
from fastapi.responses import StreamingResponse

from app.dependencies.auth import verify_jwt
from app.services.prometheus import stream_chat

router = APIRouter(dependencies=[Depends(verify_jwt)])


@router.get("/chat")
async def chat(
    message: str = Query(..., description="User message to Prometheus"),
    claims: dict[str, Any] = Depends(verify_jwt),
) -> StreamingResponse:
    """Stream a Prometheus AI response as Server-Sent Events.

    The user ID is sourced from the JWT subject claim — never from the caller.
    Each event is ``data: <text chunk>\\n\\n``.
    The stream terminates with ``data: [DONE]\\n\\n``.
    """
    user_id: str = claims.get("sub", "")
    if not user_id:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Token missing subject claim",
        )
    return StreamingResponse(
        stream_chat(user_id, message),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "X-Accel-Buffering": "no",
        },
    )
