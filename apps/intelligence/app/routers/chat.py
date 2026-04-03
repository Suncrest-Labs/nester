"""Streaming SSE chat endpoint for Prometheus AI."""

from fastapi import APIRouter, Query
from fastapi.responses import StreamingResponse

from app.services.prometheus import stream_chat

router = APIRouter()


@router.get("/chat")
async def chat(
    userId: str = Query(..., description="Nester user ID or wallet address"),
    message: str = Query(..., description="User message to Prometheus"),
) -> StreamingResponse:
    """Stream a Prometheus AI response as Server-Sent Events.

    The client should open this with `EventSource` or `fetch` + `ReadableStream`.
    Each event is `data: <text chunk>\\n\\n`.
    The stream terminates with `data: [DONE]\\n\\n`.
    """
    return StreamingResponse(
        stream_chat(userId, message),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "X-Accel-Buffering": "no",  # disable Nginx buffering for SSE
        },
    )
