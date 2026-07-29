"""Pydantic models for the AI rebalancing suggestion engine."""

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

Urgency = Literal["low", "medium", "high"]


class ProtocolAllocation(BaseModel):
    """A single protocol's share of a vault's current allocation."""

    protocol: str
    percentage: float = Field(ge=0, le=100)
    apy: float = Field(ge=0)


class RebalanceSuggestRequest(BaseModel):
    vault_id: str
    vault_balance_usd: float = Field(ge=0)
    allocations: list[ProtocolAllocation]
    # Minimum APY delta (percentage points) required to trigger a suggestion.
    threshold_pct: float = 5.0


class RebalanceAction(BaseModel):
    model_config = ConfigDict(populate_by_name=True)

    protocol: str
    action: Literal["increase", "reduce", "hold"]
    from_pct: float = Field(alias="from", ge=0, le=100)
    to_pct: float = Field(alias="to", ge=0, le=100)
    reason: str


class RebalanceSuggestResponse(BaseModel):
    vault_id: str
    should_rebalance: bool
    urgency: Urgency
    current_weighted_apy: float
    optimal_weighted_apy: float
    yield_improvement_usd: float
    actions: list[RebalanceAction]
    rationale: str
    confidence: float = Field(ge=0, le=1)


class RebalanceExecuteRequest(BaseModel):
    vault_id: str
    source_account: str
    actions: list[RebalanceAction]
    network: Literal["testnet", "public"] = "testnet"


class RebalanceExecuteResponse(BaseModel):
    vault_id: str
    unsigned_transaction_xdr: str
    network_passphrase: str
    source_account: str
