from typing import Any

from fastapi import APIRouter, Depends

from app.dependencies.auth import verify_jwt
from app.models.savings import SavingsPlanRequest, SavingsPlanResponse
from app.services.savings_service import savings_service

router = APIRouter(dependencies=[Depends(verify_jwt)])


@router.post("/savings-plan", response_model=SavingsPlanResponse)
async def create_savings_plan(
    request: SavingsPlanRequest,
    claims: dict[str, Any] = Depends(verify_jwt),
) -> Any:
    """Generate a concrete, personalized deposit schedule based on user goals."""
    return await savings_service.generate_plan(request)
