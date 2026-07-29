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
    """
    Confirm and create a goal from extracted data.

    Validates the extracted goal data and prepares it for creation.
    """
    goal_data = request.goal_data

    # Validate required fields
    required = ["name", "target_amount", "deadline", "category"]
    missing = [f for f in required if f not in goal_data]
    if missing:
        raise HTTPException(status_code=400, detail=f"Missing: {', '.join(missing)}")

    # Re-validate with GoalExtractor
    from app.services.goal_extractor import ExtractedGoal, GoalExtractor

    try:
        extractor = GoalExtractor()

        # Convert dict to ExtractedGoal
        extracted = ExtractedGoal(
            name=goal_data["name"],
            target_amount=float(goal_data["target_amount"]),
            deadline=goal_data["deadline"],
            category=goal_data.get("category", "savings"),
            initial_deposit=float(goal_data.get("initial_deposit", 0)),
            is_recurring=bool(goal_data.get("is_recurring", False)),
            recurring_amount=float(goal_data.get("recurring_amount"))
            if goal_data.get("recurring_amount")
            else None,
        )

        # Run validation
        result = extractor._validate_and_resolve(extracted, "UTC")

        if not result.success:
            raise HTTPException(
                status_code=400,
                detail=result.ambiguity.message if result.ambiguity else result.error,
            )

        # TODO: Replace with actual Go service call via relay
        return {
            "success": True,
            "message": "Goal validated successfully",
            "goal_id": f"goal_{datetime.now().strftime('%Y%m%d_%H%M%S')}",
            "goal": result.extracted.model_dump() if result.extracted else goal_data,
        }

    except ValueError as e:
        logger.error(f"Validation error: {e}")
        raise HTTPException(status_code=400, detail=f"Invalid data: {str(e)}")
    except Exception as e:
        logger.error(f"Goal creation failed: {e}")
        raise HTTPException(status_code=500, detail="Failed to create goal.")
