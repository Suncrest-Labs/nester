"""AI rebalancing suggestion and execution endpoints."""

from typing import Any

from fastapi import APIRouter, Depends, HTTPException, status

from app.dependencies.auth import verify_jwt
from app.models.rebalance import (
    RebalanceExecuteRequest,
    RebalanceExecuteResponse,
    RebalanceSuggestRequest,
    RebalanceSuggestResponse,
)
from app.services.rebalance_engine import rebalance_engine

router = APIRouter(dependencies=[Depends(verify_jwt)])


@router.post("/vaults/{vault_id}/rebalance/suggest", response_model=RebalanceSuggestResponse)
async def suggest_rebalance(
    vault_id: str,
    body: RebalanceSuggestRequest,
    claims: dict[str, Any] = Depends(verify_jwt),  # noqa: ARG001
) -> RebalanceSuggestResponse:
    """Analyze a vault's allocation and return a risk-adjusted rebalance suggestion."""
    if body.vault_id != vault_id:
        body = body.model_copy(update={"vault_id": vault_id})
    return await rebalance_engine.analyze(body)


@router.post("/vaults/{vault_id}/rebalance/execute", response_model=RebalanceExecuteResponse)
async def execute_rebalance(
    vault_id: str,
    body: RebalanceExecuteRequest,
    claims: dict[str, Any] = Depends(verify_jwt),  # noqa: ARG001
) -> RebalanceExecuteResponse:
    """Build an unsigned Stellar transaction for a user-approved rebalance."""
    if body.vault_id != vault_id:
        body = body.model_copy(update={"vault_id": vault_id})
    try:
        return rebalance_engine.build_unsigned_transaction(body)
    except ValueError as exc:
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail=str(exc)) from exc
