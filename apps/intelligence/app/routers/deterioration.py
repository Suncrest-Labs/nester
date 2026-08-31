"""API surface for predictive protocol-health deterioration summaries (#857).

The deterioration score itself is computed on the Go side
(`internal/scheduler/deterioration_score.go`); this endpoint only narrates an
already-scored assessment the Go service passes in.
"""

from typing import Any

from fastapi import APIRouter, Depends

from app.dependencies.auth import verify_jwt
from app.models.deterioration import DeteriorationSummaryRequest, DeteriorationSummaryResponse
from app.services import deterioration_summary

router = APIRouter(dependencies=[Depends(verify_jwt)])


@router.post("/protocol-deterioration-summary", response_model=DeteriorationSummaryResponse)
async def summarize_protocol_deterioration(
    request: DeteriorationSummaryRequest,
    claims: dict[str, Any] = Depends(verify_jwt),
) -> Any:
    """Have Prometheus narrate an already-computed deterioration assessment.

    The assessment (probability, level, and every indicator value) is
    produced entirely by the deterministic Go-side scoring model; this
    endpoint only narrates it in plain language and is validated to never
    introduce a number absent from it (see `app.services.deterioration_summary`).
    """
    return await deterioration_summary.summarize_assessment(
        request.assessment, audience=request.audience
    )
