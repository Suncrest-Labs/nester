"""WebSocket chat endpoint for Prometheus AI."""

from typing import Any, Optional

import jwt
from fastapi import APIRouter, WebSocket, WebSocketDisconnect

from app.config import settings
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
        claims: dict[str, Any] = jwt.decode(
            token,
            settings.jwt_secret,
            algorithms=["HS256"],
        )
    except jwt.ExpiredSignatureError:
        await websocket.send_json({"type": "auth_error", "message": "Token expired"})
        await websocket.close(code=1008)
        return None
    except jwt.InvalidTokenError:
        await websocket.send_json({"type": "auth_error", "message": "Invalid token"})
        await websocket.close(code=1008)
        return None

    user_id: str = claims.get("sub", "")
    if not user_id:
        await websocket.send_json({"type": "auth_error", "message": "Token missing subject"})
        await websocket.close(code=1008)
        return None

    await websocket.send_json({"type": "auth_success"})
    return user_id


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

            async for chunk in stream_chat(user_id, message):
                # Strip SSE "data: " prefix — WS clients get raw text
                if chunk.startswith("data: "):
                    chunk = chunk[6:].rstrip("\n")
                if chunk:
                    await websocket.send_text(chunk)

    except WebSocketDisconnect:
        pass
    except Exception:
        try:
            await websocket.send_json({"type": "error", "message": "Internal error"})
        except Exception:
            pass
