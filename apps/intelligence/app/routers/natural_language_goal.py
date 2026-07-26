"""Natural Language Goal Creation Router."""

import logging
from datetime import datetime
from typing import Any

from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel, Field

from app.dependencies.auth import verify_jwt
from app.services.goal_extractor import GoalExtractor

logger = logging.getLogger(__name__)

router = APIRouter(dependencies=[Depends(verify_jwt)])


class NaturalLanguageGoalRequest(BaseModel):
    message: str = Field(..., max_length=10000)
    timezone: str = "UTC"


class NaturalLanguageGoalResponse(BaseModel):
    success: bool
    extracted: dict[str, Any] | None = None
    ambiguity: dict[str, Any] | None = None
    error: str | None = None
    confirmation_required: bool = False


class ConfirmGoalRequest(BaseModel):
    goal_data: dict[str, Any]


@router.post("/extract-goal", response_model=NaturalLanguageGoalResponse)
async def extract_goal_from_natural_language(
    request: NaturalLanguageGoalRequest,
    claims: dict[str, Any] = Depends(verify_jwt),
) -> Any:
    extractor = GoalExtractor()
    import asyncio
    result = await asyncio.to_thread(extractor.extract, request.message, request.timezone)

    return NaturalLanguageGoalResponse(
        success=result.success,
        extracted=result.extracted.model_dump() if result.extracted else None,
        ambiguity=result.ambiguity.model_dump() if result.ambiguity else None,
        error=result.error,
        confirmation_required=result.success and result.extracted is not None,
    )


@router.post("/confirm-goal")
async def confirm_and_create_goal(
    request: ConfirmGoalRequest,
    claims: dict[str, Any] = Depends(verify_jwt),
) -> Any:
    goal_data = request.goal_data

    required = ["name", "target_amount", "deadline", "category"]
    missing = [f for f in required if f not in goal_data]
    if missing:
        raise HTTPException(status_code=400, detail=f"Missing: {', '.join(missing)}")

    # TODO: Replace with actual Go service call via relay
    raise HTTPException(
        status_code=501,
        detail="Goal creation via API is not yet implemented. Please use the form."
    )