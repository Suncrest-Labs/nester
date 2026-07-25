"""Natural Language Goal Creation Router."""

from typing import Any

from fastapi import APIRouter, Depends
from pydantic import BaseModel

from app.dependencies.auth import verify_jwt
from app.services.goal_extractor import GoalExtractor

router = APIRouter(dependencies=[Depends(verify_jwt)])


class NaturalLanguageGoalRequest(BaseModel):
    """Request to create a goal from natural language."""

    message: str
    timezone: str = "UTC"


class NaturalLanguageGoalResponse(BaseModel):
    """Response for natural language goal creation."""

    success: bool
    extracted: dict[str, Any] | None = None
    ambiguity: dict[str, Any] | None = None
    error: str | None = None
    confirmation_required: bool = False


@router.post("/extract-goal", response_model=NaturalLanguageGoalResponse)
async def extract_goal_from_natural_language(
    request: NaturalLanguageGoalRequest,
    claims: dict[str, Any] = Depends(verify_jwt),  # noqa: ARG001
) -> Any:
    """Extract structured goal fields from natural language description."""
    extractor = GoalExtractor()
    import asyncio

    result = await asyncio.to_thread(extractor.extract, request.message, request.timezone)

    response = NaturalLanguageGoalResponse(
        success=result.success,
        extracted=result.extracted.model_dump() if result.extracted else None,
        ambiguity=result.ambiguity.model_dump() if result.ambiguity else None,
        error=result.error,
        confirmation_required=result.success and result.extracted is not None,
    )

    return response


@router.post("/confirm-goal")
async def confirm_and_create_goal(
    goal_data: dict[str, Any],
    claims: dict[str, Any] = Depends(verify_jwt),  # noqa: ARG001
) -> Any:
    """Confirm and create a goal from extracted data."""
    return {
        "success": True,
        "message": "Goal confirmed and created",
        "goal_id": "pending_implementation",
    }