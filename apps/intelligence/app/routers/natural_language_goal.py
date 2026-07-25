"""
Natural Language Goal Creation Router

Provides endpoints for creating savings goals from natural language input.
"""

from typing import Any
from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel

from app.dependencies.auth import verify_jwt
from app.services.goal_extractor import GoalExtractor, GoalExtractionResult

router = APIRouter(dependencies=[Depends(verify_jwt)])


class NaturalLanguageGoalRequest(BaseModel):
    """Request to create a goal from natural language"""
    message: str
    timezone: str = "UTC"


class NaturalLanguageGoalResponse(BaseModel):
    """Response for natural language goal creation"""
    success: bool
    extracted: dict[str, Any] | None = None
    ambiguity: dict[str, Any] | None = None
    error: str | None = None
    confirmation_required: bool = False


@router.post("/extract-goal", response_model=NaturalLanguageGoalResponse)
async def extract_goal_from_natural_language(
    request: NaturalLanguageGoalRequest,
    claims: dict[str, Any] = Depends(verify_jwt),
) -> Any:
    """
    Extract structured goal fields from natural language description.
    
    This endpoint uses Claude to parse natural language and extract:
    - Goal name
    - Target amount
    - Deadline
    - Category
    - Initial deposit (optional)
    - Recurring plan (optional)
    
    Returns either:
    1. A structured goal ready for confirmation
    2. An ambiguity that needs clarification
    3. An error
    """
    extractor = GoalExtractor()
    result = extractor.extract(request.message, request.timezone)
    
    response = NaturalLanguageGoalResponse(
        success=result.success,
        extracted=result.extracted.model_dump() if result.extracted else None,
        ambiguity=result.ambiguity.model_dump() if result.ambiguity else None,
        error=result.error,
        confirmation_required=result.success and result.extracted is not None
    )
    
    return response


@router.post("/confirm-goal")
async def confirm_and_create_goal(
    goal_data: dict[str, Any],
    claims: dict[str, Any] = Depends(verify_jwt),
) -> Any:
    """
    Confirm and create a goal from extracted data.
    
    This is called after the user confirms the extracted goal.
    The actual creation goes through the Go backend service.
    """
    # This will call the Go service via the relay
    # Implementation will be added in the next phase
    # For now, return a placeholder
    return {
        "success": True,
        "message": "Goal confirmed and created",
        "goal_id": "pending_implementation"
    }