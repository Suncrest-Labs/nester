"""Conversation rating and feedback capture endpoints (#926).

Allows users to submit thumbs-up/down ratings with optional comments for
conversation turns, and view their feedback history. This data is used to
track response quality over time and feed into evaluation datasets.
"""

import uuid
from datetime import datetime, timezone
from typing import Any

from fastapi import APIRouter, Depends, HTTPException, Request, status
from slowapi import Limiter
from slowapi.util import get_remote_address

from app.dependencies.auth import verify_jwt
from app.models.feedback import (
    FeedbackEntry,
    FeedbackListResponse,
    FeedbackRequest,
    FeedbackResponse,
)
from app.services.feedback_store import FeedbackEntryDict
from app.services.feedback_store import store as feedback_store

router = APIRouter(dependencies=[Depends(verify_jwt)])

_limiter = Limiter(key_func=get_remote_address)


def _require_user_id(claims: dict[str, Any]) -> str:
    user_id = claims.get("sub", "")
    if not user_id:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="User ID not found in token",
        )
    return str(user_id)


@router.post("/feedback", response_model=FeedbackResponse, status_code=status.HTTP_201_CREATED)
@_limiter.limit("60/minute")
async def submit_feedback(
    request: Request,
    body: FeedbackRequest,
    claims: dict[str, Any] = Depends(verify_jwt),
) -> FeedbackResponse:
    """Submit a thumbs-up or thumbs-down rating for a conversation turn.

    The rating is stored per-user and includes an optional free-text comment
    and a reference to the conversation turn being rated.
    """
    user_id = _require_user_id(claims)
    feedback_id = str(uuid.uuid4())

    now = datetime.now(timezone.utc).isoformat()

    entry: FeedbackEntryDict = {
        "id": feedback_id,
        "rating": body.rating,
        "comment": body.comment,
        "conversation_id": body.conversation_id,
        "user_id": user_id,
        "created_at": now,
    }

    feedback_store.submit(user_id, entry)

    return FeedbackResponse(
        ok=True,
        feedback_id=feedback_id,
    )


@router.get("/feedback", response_model=FeedbackListResponse)
@_limiter.limit("30/minute")
async def get_feedback(
    request: Request,
    claims: dict[str, Any] = Depends(verify_jwt),
) -> FeedbackListResponse:
    """Return the authenticated user's feedback history.

    Entries are returned newest-first so the most recent ratings appear
    at the top of the list.
    """
    user_id = _require_user_id(claims)
    entries = feedback_store.get_all(user_id)

    # Sort newest-first by created_at
    entries.sort(key=lambda e: e.get("created_at", ""), reverse=True)

    feedback_entries = [
        FeedbackEntry(
            id=e["id"],
            rating=e["rating"],
            comment=e.get("comment", ""),
            conversation_id=e.get("conversation_id", ""),
            user_id=e["user_id"],
            created_at=e["created_at"],
        )
        for e in entries
    ]

    return FeedbackListResponse(
        feedback_entries=feedback_entries,
        total=len(feedback_entries),
    )
