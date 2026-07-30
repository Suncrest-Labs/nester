"""WebSocket chat endpoint for Prometheus AI."""

import uuid
from typing import Any, Optional

import jwt
from fastapi import APIRouter, WebSocket, WebSocketDisconnect

from app.config import settings
from app.services import guardrails
from app.services.prometheus import stream_chat

router = APIRouter()


async def _authenticate(websocket: WebSocket) -> Optional[str]:
    """Validate the Bearer token sent in the first message.

    Returns the authenticated user_id (JWT sub) or None if auth fails.
    Sends a JSON error frame before returning None so the client can show a
    meaningful message.
    """
    try:
        msg: dict[str, Any] = await websocket.receive_json()
    except Exception:
        await websocket.close(code=1003)
        return None

    token = msg.get("token", "")
    if not token:
        await websocket.send_json({"type": "auth_error", "message": "Missing token"})
        await websocket.close(code=1008)
        return None

    try:
        payload = jwt.decode(token, settings.anthropic_api_key, algorithms=["HS256"])
        user_id = payload.get("sub")
        if not user_id:
            raise ValueError("missing sub claim")
        return user_id
    except Exception:
        await websocket.send_json({"type": "auth_error", "message": "Invalid token"})
        await websocket.close(code=1008)
        return None


@router.websocket("/ws/chat")
async def websocket_chat(websocket: WebSocket) -> None:
    await websocket.accept()

    user_id = await _authenticate(websocket)
    if user_id is None:
        return

    try:
        while True:
            data: dict[str, Any] = await websocket.receive_json()
            message: str = data.get("message", "").strip()
            if not message:
                await websocket.send_json({"type": "error", "message": "message is required"})
                continue

            if len(message) > guardrails.MAX_USER_MESSAGE_CHARS:
                await websocket.send_json(
                    {"type": "error", "message": "message is too long"}
                )
                continue

            request_id = str(uuid.uuid4())

            async for chunk in stream_chat(
                user_id, message, request_id=request_id
            ):
                # Strip SSE "data: " prefix — WS clients get raw text
                if chunk.startswith("data: "):
                    chunk = chunk[6:].rstrip("\n")
                await websocket.send_text(chunk)

    except WebSocketDisconnect:
        pass
    except Exception:
        try:
            await websocket.send_json({"type": "error", "message": "Internal error"})
        except Exception:
            pass
