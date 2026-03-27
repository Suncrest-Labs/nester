from fastapi import APIRouter, HTTPException
from pydantic import BaseModel
from typing import List, Optional
from app.services.anomaly_scorer import AnomalyScorer, RiskEvaluation

router = APIRouter(prefix="/api/v1/risk", tags=["risk"])
scorer = AnomalyScorer()

class RiskEvaluationRequest(BaseModel):
    user_id: str
    amount: float
    history_avg: float
    velocity_24h: int
    is_novel_account: bool
    destination_account: str

class RiskAlert(BaseModel):
    user_id: str
    score: int
    rule: str
    explanation: str
    timestamp: str

@router.post("/evaluate", response_model=RiskEvaluation)
async def evaluate_risk(request: RiskEvaluationRequest):
    return scorer.evaluate(
        amount=request.amount,
        history_avg=request.history_avg,
        velocity_24h=request.velocity_24h,
        is_novel_account=request.is_novel_account
    )

@router.get("/users/{user_id}/alerts", response_model=List[RiskAlert])
async def get_alerts(user_id: str):
    # Placeholder for alert history
    # In a real scenario, this would query a database
    return []
