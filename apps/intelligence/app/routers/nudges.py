from typing import Any

from fastapi import APIRouter, Depends, HTTPException

from app.dependencies.auth import verify_jwt
from app.models.nudge import NudgeCopyRequest, NudgeCopyResponse
from app.services.prometheus import generate_nudge_copy

router = APIRouter(dependencies=[Depends(verify_jwt)])


@router.post("/copy", response_model=NudgeCopyResponse)
async def generate_copy(
    request: NudgeCopyRequest,
    claims: dict[str, Any] = Depends(verify_jwt),  # noqa: ARG001
) -> NudgeCopyResponse:
    try:
        return await generate_nudge_copy(
            nudge_type=request.nudge_type,
            facts=request.facts,
            segment=request.segment,
            request_id=request.request_id,
        )
    except Exception as exc:
        raise HTTPException(status_code=500, detail=str(exc)) from exc
