"""Streaming SSE chat endpoint for Prometheus AI."""

from fastapi import APIRouter
from fastapi.responses import StreamingResponse
from pydantic import BaseModel

from app.services.prometheus import stream_chat

router = APIRouter()


class ChatRequest(BaseModel):
    userId: str
    message: str


@router.post("/chat")
async def chat(request: ChatRequest) -> StreamingResponse:
    """Stream a Prometheus AI response as Server-Sent Events.

    The client should open this with `fetch` + `ReadableStream`.
    Each event is `data: <text chunk>\\n\\n`.
    The stream terminates with `data: [DONE]\\n\\n`.
    """
    return StreamingResponse(
        stream_chat(request.userId, request.message),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "X-Accel-Buffering": "no",  # disable Nginx buffering for SSE
        },
    )
