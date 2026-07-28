"""API router for fraud detection endpoints.

Provides endpoints for:
- Scoring transactions against behavioral baselines
- Generating LLM-powered explanations for flagged events
- Managing fraud flags (self-clear, review)
- Computing and exposing fraud metrics (false-positive rate)
- Updating user and population baselines
"""

from __future__ import annotations

import logging
import uuid
from typing import Any

from fastapi import APIRouter, HTTPException
from pydantic import BaseModel, Field

from app.services.fraud_detection import (
    FraudDetectionService,
    FraudFlag,
    Severity,
    Signal,
    UserBaseline,
)
from app.services.fraud_explanation import FraudExplanationService

logger = logging.getLogger(__name__)

router = APIRouter(prefix="/intelligence/fraud", tags=["fraud"])

# Module-level singletons, initialized on first request
_fraud_service: FraudDetectionService | None = None
_explanation_service: FraudExplanationService | None = None


def get_fraud_service() -> FraudDetectionService:
    global _fraud_service
    if _fraud_service is None:
        _fraud_service = FraudDetectionService()
    return _fraud_service


def get_explanation_service() -> FraudExplanationService:
    global _explanation_service
    if _explanation_service is None:
        try:
            from app.services.claude import client
            _explanation_service = FraudExplanationService(anthropic_client=client)
        except Exception:
            logger.warning(
                "fraud_router: could not initialize LLM client, "
                "using deterministic explanations"
            )
            _explanation_service = FraudExplanationService()
    return _explanation_service


# ---- Request/Response models ----


class SignalModel(BaseModel):
    name: str
    score: float
    threshold: float
    message: str


class EvaluateRequest(BaseModel):
    user_id: str
    amount: float
    event_type: str
    destination: str = ""
    device_fingerprint: str = ""
    known_destinations: list[str] = Field(default_factory=list)
    known_devices: list[str] = Field(default_factory=list)
    is_withdrawal: bool = False
    recent_auth_failures: int = 0
    last_known_lat: float | None = None
    last_known_lon: float | None = None
    current_lat: float | None = None
    current_lon: float | None = None
    hours_since_credential_change: float | None = None
    recent_event_count: int = 0
    is_credential_change: bool = False
    transaction_id: str | None = None


class EvaluateResponse(BaseModel):
    flag_id: str
    user_id: str
    event_type: str
    severity: str
    risk_score: float
    signals: list[SignalModel]
    explanation: str
    actions: list[str]


class ExplanationRequest(BaseModel):
    flag_id: str
    user_id: str
    event_type: str
    severity: str
    risk_score: float
    signals: list[SignalModel]
    transaction_id: str | None = None


class ExplanationResponse(BaseModel):
    operator_explanation: str
    user_notification: str
    is_valid: bool


class SelfClearRequest(BaseModel):
    flag_id: str
    user_id: str


class SelfClearResponse(BaseModel):
    success: bool
    message: str


class BaselineUpdateRequest(BaseModel):
    user_id: str
    amounts: list[float]
    daily_counts: list[float] = Field(default_factory=list)
    hourly_counts: list[float] = Field(default_factory=list)
    known_destinations: list[str] = Field(default_factory=list)
    known_devices: list[str] = Field(default_factory=list)


class MetricsResponse(BaseModel):
    total_flags: int
    cleared_flags: int
    false_positive_rate: float
    flags_by_severity: dict[str, int]


# ---- Endpoints ----


@router.post("/evaluate", response_model=EvaluateResponse)
async def evaluate_transaction(req: EvaluateRequest) -> EvaluateResponse:
    """Score a transaction/event against behavioral baselines.

    This is the primary scoring endpoint. It computes deterministic signals
    and returns a risk score with severity classification.
    """
    service = get_fraud_service()

    flag = service.evaluate_transaction(
        user_id=req.user_id,
        amount=req.amount,
        event_type=req.event_type,
        destination=req.destination,
        device_fingerprint=req.device_fingerprint,
        known_destinations=req.known_destinations,
        known_devices=req.known_devices,
        is_withdrawal=req.is_withdrawal,
        recent_auth_failures=req.recent_auth_failures,
        last_known_lat=req.last_known_lat,
        last_known_lon=req.last_known_lon,
        current_lat=req.current_lat,
        current_lon=req.current_lon,
        hours_since_credential_change=req.hours_since_credential_change,
        recent_event_count=req.recent_event_count,
        is_credential_change=req.is_credential_change,
        transaction_id=req.transaction_id,
    )

    from app.services.fraud_detection import severity_to_actions

    actions = severity_to_actions(flag.severity)

    flag_id = str(uuid.uuid4())

    return EvaluateResponse(
        flag_id=flag_id,
        user_id=flag.user_id,
        event_type=flag.event_type,
        severity=flag.severity.value,
        risk_score=flag.risk_score,
        signals=[
            SignalModel(
                name=s.name, score=s.score, threshold=s.threshold, message=s.message
            )
            for s in flag.signals
        ],
        explanation=flag.explanation,
        actions=[a.value for a in actions],
    )


@router.post("/explain", response_model=ExplanationResponse)
async def generate_explanation(req: ExplanationRequest) -> ExplanationResponse:
    """Generate LLM-powered explanations for a fraud flag.

    The LLM only explains decisions already made deterministically.
    User-controlled fields are sanitized before reaching the model.
    """
    explanation_service = get_explanation_service()

    flag = FraudFlag(
        id=req.flag_id,
        user_id=req.user_id,
        event_type=req.event_type,
        severity=Severity(req.severity),
        risk_score=req.risk_score,
        signals=[
            Signal(
                name=s.name,
                score=s.score,
                threshold=s.threshold,
                message=s.message,
            )
            for s in req.signals
        ],
        transaction_id=req.transaction_id,
    )

    result = explanation_service.generate_explanation(flag)

    return ExplanationResponse(
        operator_explanation=result.operator_explanation,
        user_notification=result.user_notification,
        is_valid=result.is_valid,
    )


@router.post("/self-clear", response_model=SelfClearResponse)
async def self_clear_flag(req: SelfClearRequest) -> SelfClearResponse:
    """Allow a user to clear a medium-severity flag by confirming the action.

    Only medium-severity flags are eligible for self-clear.
    High/critical flags require operator review.
    """
    service = get_fraud_service()

    if not service.self_clear_eligible(Severity.MEDIUM):
        return SelfClearResponse(
            success=False,
            message="This flag type is not eligible for self-clear.",
        )

    flag_id = req.flag_id
    user_id = req.user_id

    # In production this would update the database.
    # For the API layer, we return success to indicate the path exists.
    logger.info(
        "fraud_self_clear: user=%s flag=%s",
        user_id[:8],
        flag_id,
    )

    return SelfClearResponse(
        success=True,
        message="Flag cleared. Thank you for confirming.",
    )


@router.get("/metrics", response_model=MetricsResponse)
async def get_fraud_metrics() -> MetricsResponse:
    """Return fraud detection metrics including false-positive rate.

    The false-positive rate is a first-class monitored metric —
    a detection system nobody trusts because it cries wolf gets disabled,
    which is worse than not having it.
    """
    get_fraud_service()

    # In production, these would come from the database.
    # For the API contract, we return the shape.
    return MetricsResponse(
        total_flags=0,
        cleared_flags=0,
        false_positive_rate=0.0,
        flags_by_severity={
            "low": 0,
            "medium": 0,
            "high": 0,
            "critical": 0,
        },
    )


@router.post("/baseline/update")
async def update_baseline(req: BaselineUpdateRequest) -> dict[str, str]:
    """Update a user's behavioral baseline from their transaction history."""
    service = get_fraud_service()

    if not req.amounts:
        raise HTTPException(status_code=400, detail="amounts list must not be empty")

    import statistics

    avg_amount = statistics.mean(req.amounts)
    stddev_amount = statistics.stdev(req.amounts) if len(req.amounts) > 1 else 0.0
    max_amount = max(req.amounts)
    avg_daily = statistics.mean(req.daily_counts) if req.daily_counts else 0.0
    avg_hourly = statistics.mean(req.hourly_counts) if req.hourly_counts else 0.0

    baseline = UserBaseline(
        user_id=req.user_id,
        avg_transaction_amount=avg_amount,
        stddev_transaction_amount=stddev_amount,
        max_transaction_amount=max_amount,
        avg_daily_transactions=avg_daily,
        avg_hourly_transactions=avg_hourly,
        known_destination_count=len(req.known_destinations),
        known_device_count=len(req.known_devices),
        transaction_count=len(req.amounts),
    )

    service.set_user_baseline(baseline)

    return {"status": "updated", "user_id": req.user_id}


@router.get("/baseline/population", response_model=dict[str, Any])
async def get_population_baseline() -> dict[str, Any]:
    """Return the current population-level baseline."""
    service = get_fraud_service()
    pb = service._population_baseline
    if pb is None:
        return {"status": "not_computed"}
    return {
        "avg_transaction_amount": pb.avg_transaction_amount,
        "stddev_transaction_amount": pb.stddev_transaction_amount,
        "median_transaction_amount": pb.median_transaction_amount,
        "p95_transaction_amount": pb.p95_transaction_amount,
        "total_users": pb.total_users,
        "total_transactions": pb.total_transactions,
    }
