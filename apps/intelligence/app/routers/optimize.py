"""API surface for the yield optimization engine (#848)."""

from typing import Any

from fastapi import APIRouter, Depends

from app.dependencies.auth import verify_jwt
from app.models.optimization import OptimizationRequest, OptimizationResponse
from app.services import yield_explanation, yield_optimizer

router = APIRouter(dependencies=[Depends(verify_jwt)])


@router.post("/yield-optimization", response_model=OptimizationResponse)
async def optimize_yield_allocation(
    request: OptimizationRequest,
    claims: dict[str, Any] = Depends(verify_jwt),
) -> Any:
    """Compute the constraint-based, risk-adjusted-optimal allocation across
    the given yield sources, and have Prometheus explain the result.

    The allocation is produced entirely by a pure, deterministic optimizer
    (`app.services.yield_optimizer.optimize`); the LLM only narrates the
    already-computed result in `explanation` and is validated to never
    introduce a number absent from it (see `app.services.yield_explanation`).
    If the given constraints are jointly infeasible, `result.feasible` is
    False and `result.infeasibility_reasons` explains why -- no partial or
    constraint-violating allocation is ever returned.
    """
    result = yield_optimizer.optimize(
        request.sources, request.constraints, request.investable_amount_usd
    )
    explanation = await yield_explanation.explain_allocation(result)
    return OptimizationResponse(result=result, explanation=explanation)
