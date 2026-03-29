from typing import List
from pydantic import BaseModel

class RiskEvaluation(BaseModel):
    score: int
    triggered_rules: List[str]
    recommended_action: str
    explanation: str

class AnomalyScorer:
    def __init__(self):
        self.thresholds = {
            "allow": 30,
            "flag": 70,
            "hold": 90,
            "block": 100
        }

    def evaluate(
        self,
        amount: float,
        history_avg: float,
        velocity_24h: int,
        is_novel_account: bool
    ) -> RiskEvaluation:
        score = 0
        triggered_rules = []

        # 1. Amount Deviation Score
        if history_avg > 0:
            deviation = (amount - history_avg) / history_avg
            if deviation > 2.0:  # 200% above average
                score += 40
                triggered_rules.append("SIGNIFICANT_AMOUNT_DEVIATION")
            elif deviation > 0.5:  # 50% above average
                score += 15
                triggered_rules.append("MODERATE_AMOUNT_DEVIATION")

        # 2. Velocity Score
        if velocity_24h > 10:
            score += 30
            triggered_rules.append("HIGH_VELOCITY_24H")
        elif velocity_24h > 5:
            score += 10
            triggered_rules.append("MODERATE_VELOCITY_24H")

        # 3. Novel Settlement Account
        if is_novel_account:
            score += 20
            triggered_rules.append("NOVEL_SETTLEMENT_ACCOUNT")

        recommended_action = self._recommend_action(score)
        explanation = self._explain(triggered_rules)

        return RiskEvaluation(
            score=score,
            triggered_rules=triggered_rules,
            recommended_action=recommended_action,
            explanation=explanation
        )

    def _recommend_action(self, score: int) -> str:
        if score < self.thresholds["allow"]:
            return "allow"
        elif score < self.thresholds["flag"]:
            return "flag"
        elif score < self.thresholds["hold"]:
            return "hold"
        else:
            return "block"

    def _explain(self, rules: List[str]) -> str:
        if not rules:
            return "Transaction appears normal based on historical patterns."
        parts = []
        if "SIGNIFICANT_AMOUNT_DEVIATION" in rules:
            parts.append("an unusually large withdrawal compared to your history")
        elif "MODERATE_AMOUNT_DEVIATION" in rules:
            parts.append("an amount slightly higher than your usual pattern")
        if "HIGH_VELOCITY_24H" in rules:
            parts.append("a high frequency of transactions in a short period")
        elif "MODERATE_VELOCITY_24H" in rules:
            parts.append("an increased transaction frequency")
        if "NOVEL_SETTLEMENT_ACCOUNT" in rules:
            parts.append("the use of a new settlement destination")
        return "This transaction was flagged due to " + ", ".join(parts) + "."
