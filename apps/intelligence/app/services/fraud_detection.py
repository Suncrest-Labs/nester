"""Fraud and anomaly detection engine for transactions and account events.

This module scores transactions and account events against per-user behavioral
baselines and population norms. The detection is deterministic and auditable —
every flag has stated reasons and contributing signals.

The hybrid architecture:
- Fast deterministic signals (velocity, new-destination, post-credential-change)
  are computed inline in the Go backend for low latency.
- Heavier behavioral modeling (population norms, statistical baselines, clustering)
  lives here in the Python intelligence service for better ML/stats tooling.

This service provides:
- Population-level baseline computation
- Composite risk scoring combining multiple signals
- Severity classification with graduated response
- False-positive rate monitoring
- User self-clear path for medium-severity flags
"""

from __future__ import annotations

import math
from dataclasses import dataclass, field
from datetime import datetime, timezone
from enum import Enum
from typing import Any


class Severity(str, Enum):
    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"
    CRITICAL = "critical"


class FlagStatus(str, Enum):
    OPEN = "open"
    CLEARED = "cleared"
    CONFIRMED = "confirmed"
    AUTO_CLEARED = "auto_cleared"


class ActionType(str, Enum):
    LOG = "log"
    STEP_UP_AUTH = "step_up_auth"
    HOLD = "hold"
    BLOCK = "block"


@dataclass(slots=True)
class Signal:
    """A single deterministic detection signal contributing to a risk score."""

    name: str
    score: float
    threshold: float
    message: str
    details: dict[str, Any] = field(default_factory=dict)


@dataclass(slots=True)
class FraudFlag:
    """A detected anomalous event with its contributing signals."""

    id: str
    user_id: str
    transaction_id: str | None = None
    event_type: str = ""
    severity: Severity = Severity.LOW
    status: FlagStatus = FlagStatus.OPEN
    signals: list[Signal] = field(default_factory=list)
    risk_score: float = 0.0
    explanation: str = ""
    user_notified: bool = False
    cleared_by_user: bool = False
    created_at: datetime = field(default_factory=lambda: datetime.now(timezone.utc))


@dataclass(slots=True)
class PopulationBaseline:
    """Aggregate behavioral norms across all users."""

    avg_transaction_amount: float = 0.0
    stddev_transaction_amount: float = 0.0
    median_transaction_amount: float = 0.0
    p95_transaction_amount: float = 0.0
    avg_daily_transactions_per_user: float = 0.0
    avg_hourly_transactions_per_user: float = 0.0
    typical_hours: list[int] = field(default_factory=list)
    total_users: int = 0
    total_transactions: int = 0


@dataclass(slots=True)
class UserBaseline:
    """Per-user behavioral baseline."""

    user_id: str
    avg_transaction_amount: float = 0.0
    stddev_transaction_amount: float = 0.0
    max_transaction_amount: float = 0.0
    avg_daily_transactions: float = 0.0
    avg_hourly_transactions: float = 0.0
    known_destination_count: int = 0
    known_device_count: int = 0
    typical_hour_start: int = 0
    typical_hour_end: int = 23
    transaction_count: int = 0


# ---- Threshold constants ----

AMOUNT_DEVIATION_THRESHOLD = 3.0
MIN_BASELINE_TRANSACTIONS = 5
ABSOLUTE_MAX_AMOUNT_NEW_USER = 50000.0
NEW_DESTINATION_BASE_SCORE = 0.3
OCCASIONAL_DESTINATION_THRESHOLD = 3
AUTH_FAILURE_BURST_COUNT = 5
VELOCITY_SPIKE_MULTIPLIER = 5.0
IMPOSSIBLE_TRAVEL_MIN_KM = 500.0
IMPOSSIBLE_TRAVEL_MAX_HOURS = 1.0
POST_CREDENTIAL_CHANGE_WINDOW_HOURS = 2.0

SCORE_LOW_THRESHOLD = 0.2
SCORE_MEDIUM_THRESHOLD = 0.4
SCORE_HIGH_THRESHOLD = 0.7
SCORE_CRITICAL_THRESHOLD = 0.9


def haversine_distance(lat1: float, lon1: float, lat2: float, lon2: float) -> float:
    """Compute great-circle distance between two points in kilometers."""
    earth_radius_km = 6371.0
    lat1_r, lat2_r = math.radians(lat1), math.radians(lat2)
    dlat = math.radians(lat2 - lat1)
    dlon = math.radians(lon2 - lon1)
    a = (
        math.sin(dlat / 2) ** 2
        + math.cos(lat1_r) * math.cos(lat2_r) * math.sin(dlon / 2) ** 2
    )
    return earth_radius_km * 2 * math.atan2(math.sqrt(a), math.sqrt(1 - a))


def aggregate_score(signals: list[Signal]) -> float:
    """Combine independent signal scores using probability of union.

    Uses 1 - product(1 - score_i) for independent signals.
    """
    if not signals:
        return 0.0
    combined = 1.0
    for s in signals:
        combined *= 1.0 - s.score
    return 1.0 - combined


def score_to_severity(score: float) -> Severity:
    """Map an aggregate risk score to a severity band."""
    if score >= SCORE_CRITICAL_THRESHOLD:
        return Severity.CRITICAL
    if score >= SCORE_HIGH_THRESHOLD:
        return Severity.HIGH
    if score >= SCORE_MEDIUM_THRESHOLD:
        return Severity.MEDIUM
    return Severity.LOW


def severity_to_actions(severity: Severity) -> list[ActionType]:
    """Map severity to the graduated response actions."""
    match severity:
        case Severity.CRITICAL | Severity.HIGH:
            return [ActionType.HOLD, ActionType.STEP_UP_AUTH]
        case Severity.MEDIUM:
            return [ActionType.STEP_UP_AUTH]
        case _:
            return [ActionType.LOG]


class FraudDetectionService:
    """Scores events against per-user and population baselines.

    The service is deterministic: given the same inputs, it always produces
    the same signals, score, and severity. The LLM layer plays a supporting
    role in explaining flagged patterns, not in the block/allow decision.
    """

    def __init__(self) -> None:
        self._user_baselines: dict[str, UserBaseline] = {}
        self._population_baseline: PopulationBaseline | None = None

    def set_user_baseline(self, baseline: UserBaseline) -> None:
        self._user_baselines[baseline.user_id] = baseline

    def get_user_baseline(self, user_id: str) -> UserBaseline | None:
        return self._user_baselines.get(user_id)

    def set_population_baseline(self, baseline: PopulationBaseline) -> None:
        self._population_baseline = baseline

    def update_population_baseline(
        self, user_baselines: list[UserBaseline], all_amounts: list[float]
    ) -> PopulationBaseline:
        """Compute population-level baseline from individual user baselines."""
        if not all_amounts:
            self._population_baseline = PopulationBaseline()
            return self._population_baseline

        sorted_amounts = sorted(all_amounts)
        n = len(sorted_amounts)
        mean = sum(sorted_amounts) / n
        variance = sum((a - mean) ** 2 for a in sorted_amounts) / n
        stddev = math.sqrt(variance)
        median = sorted_amounts[n // 2]
        p95_idx = min(int(n * 0.95), n - 1)
        p95 = sorted_amounts[p95_idx]

        avg_daily = (
            sum(b.avg_daily_transactions for b in user_baselines) / len(user_baselines)
            if user_baselines
            else 0.0
        )
        avg_hourly = (
            sum(b.avg_hourly_transactions for b in user_baselines) / len(user_baselines)
            if user_baselines
            else 0.0
        )

        baseline = PopulationBaseline(
            avg_transaction_amount=mean,
            stddev_transaction_amount=stddev,
            median_transaction_amount=median,
            p95_transaction_amount=p95,
            avg_daily_transactions_per_user=avg_daily,
            avg_hourly_transactions_per_user=avg_hourly,
            total_users=len(user_baselines),
            total_transactions=n,
        )
        self._population_baseline = baseline
        return baseline

    def evaluate_transaction(
        self,
        user_id: str,
        amount: float,
        event_type: str,
        destination: str = "",
        device_fingerprint: str = "",
        known_destinations: list[str] | None = None,
        known_devices: list[str] | None = None,
        is_withdrawal: bool = False,
        recent_auth_failures: int = 0,
        last_known_lat: float | None = None,
        last_known_lon: float | None = None,
        current_lat: float | None = None,
        current_lon: float | None = None,
        hours_since_credential_change: float | None = None,
        recent_event_count: int = 0,
        is_credential_change: bool = False,
        transaction_id: str | None = None,
    ) -> FraudFlag:
        """Score a transaction/event against behavioral baselines.

        Returns a FraudFlag with signals, risk score, and severity.
        This is the core deterministic scoring function — always returns
        the same result for the same inputs.
        """
        signals: list[Signal] = []
        now = datetime.now(timezone.utc)

        # Signal 1: Amount deviation
        sig = self._check_amount_deviation(user_id, amount)
        if sig is not None:
            signals.append(sig)

        # Signal 2: New destination
        if is_withdrawal and destination:
            sig = self._check_new_destination(
                user_id, destination, known_destinations or []
            )
            if sig is not None:
                signals.append(sig)

        # Signal 3: New device
        if device_fingerprint:
            sig = self._check_new_device(device_fingerprint, known_devices or [])
            if sig is not None:
                signals.append(sig)

        # Signal 4: Auth failure burst
        sig = self._check_auth_failure_burst(user_id, recent_auth_failures)
        if sig is not None:
            signals.append(sig)

        # Signal 5: Impossible travel
        sig = self._check_impossible_travel(
            last_known_lat, last_known_lon, current_lat, current_lon
        )
        if sig is not None:
            signals.append(sig)

        # Signal 6: Post-credential-change activity
        sig = self._check_post_credential_change(
            is_credential_change, hours_since_credential_change
        )
        if sig is not None:
            signals.append(sig)

        # Signal 7: Velocity spike
        sig = self._check_velocity_spike(user_id, recent_event_count)
        if sig is not None:
            signals.append(sig)

        # Compute aggregate score
        score = aggregate_score(signals)
        severity = score_to_severity(score)
        explanation = self._build_explanation(signals, severity, score)

        flag = FraudFlag(
            id="",  # assigned by persistence layer
            user_id=user_id,
            transaction_id=transaction_id,
            event_type=event_type,
            severity=severity,
            signals=signals,
            risk_score=score,
            explanation=explanation,
            created_at=now,
        )

        return flag

    def _check_amount_deviation(
        self, user_id: str, amount: float
    ) -> Signal | None:
        baseline = self._user_baselines.get(user_id)

        if baseline is None or baseline.transaction_count < MIN_BASELINE_TRANSACTIONS:
            if amount > ABSOLUTE_MAX_AMOUNT_NEW_USER:
                return Signal(
                    name="amount_deviation",
                    score=0.5,
                    threshold=ABSOLUTE_MAX_AMOUNT_NEW_USER,
                    message="Transaction amount exceeds absolute maximum for new user",
                )
            return None

        stddev = baseline.stddev_transaction_amount
        if stddev <= 0:
            stddev = baseline.avg_transaction_amount * 0.1
            if stddev <= 0:
                return None

        deviation = (amount - baseline.avg_transaction_amount) / stddev
        if deviation > AMOUNT_DEVIATION_THRESHOLD:
            score = min(1.0, deviation / (AMOUNT_DEVIATION_THRESHOLD * 2))
            return Signal(
                name="amount_deviation",
                score=score,
                threshold=AMOUNT_DEVIATION_THRESHOLD,
                message="Transaction amount significantly deviates from user's historical norm",
            )
        return None

    def _check_new_destination(
        self, user_id: str, destination: str, known_destinations: list[str]
    ) -> Signal | None:
        baseline = self._user_baselines.get(user_id)

        if not known_destinations:
            if baseline is None or baseline.known_destination_count == 0:
                return Signal(
                    name="new_destination",
                    score=0.4,
                    threshold=0,
                    message="First withdrawal destination for this account",
                )
            return Signal(
                name="new_destination",
                score=NEW_DESTINATION_BASE_SCORE,
                threshold=0,
                message="Withdrawal to previously unseen destination",
            )

        if destination in known_destinations:
            return None

        return Signal(
            name="new_destination",
            score=NEW_DESTINATION_BASE_SCORE,
            threshold=0,
            message="Withdrawal to previously unseen destination",
        )

    def _check_new_device(
        self, device_fingerprint: str, known_devices: list[str]
    ) -> Signal | None:
        if device_fingerprint in known_devices:
            return None
        return Signal(
            name="new_device",
            score=0.25,
            threshold=0,
            message="Activity from unrecognized device",
        )

    def _check_auth_failure_burst(
        self, user_id: str, recent_failures: int
    ) -> Signal | None:
        if recent_failures >= AUTH_FAILURE_BURST_COUNT:
            score = min(1.0, recent_failures / (AUTH_FAILURE_BURST_COUNT * 2))
            return Signal(
                name="auth_failure_burst",
                score=score,
                threshold=float(AUTH_FAILURE_BURST_COUNT),
                message="Multiple authentication failures in a short window",
                details={"failure_count": recent_failures},
            )
        return None

    def _check_impossible_travel(
        self,
        last_lat: float | None,
        last_lon: float | None,
        current_lat: float | None,
        current_lon: float | None,
    ) -> Signal | None:
        if (
            last_lat is None
            or last_lon is None
            or current_lat is None
            or current_lon is None
        ):
            return None

        distance = haversine_distance(last_lat, last_lon, current_lat, current_lon)
        if distance > IMPOSSIBLE_TRAVEL_MIN_KM:
            score = min(1.0, distance / (IMPOSSIBLE_TRAVEL_MIN_KM * 2))
            return Signal(
                name="impossible_travel",
                score=score,
                threshold=IMPOSSIBLE_TRAVEL_MIN_KM,
                message="Activity from implausible location given recent activity",
                details={"distance_km": round(distance, 1)},
            )
        return None

    def _check_post_credential_change(
        self,
        is_credential_change: bool,
        hours_since_change: float | None,
    ) -> Signal | None:
        if is_credential_change:
            return None
        if (
            hours_since_change is not None
            and hours_since_change < POST_CREDENTIAL_CHANGE_WINDOW_HOURS
        ):
            score = 0.6
            return Signal(
                name="post_credential_change",
                score=score,
                threshold=POST_CREDENTIAL_CHANGE_WINDOW_HOURS,
                message="Sensitive activity following credential change",
            )
        return None

    def _check_velocity_spike(
        self, user_id: str, recent_event_count: int
    ) -> Signal | None:
        baseline = self._user_baselines.get(user_id)
        if baseline is None or baseline.avg_hourly_transactions <= 0:
            return None

        threshold = baseline.avg_hourly_transactions * VELOCITY_SPIKE_MULTIPLIER
        if recent_event_count > threshold:
            score = min(1.0, recent_event_count / (threshold * 2))
            return Signal(
                name="velocity_spike",
                score=score,
                threshold=threshold,
                message="Transaction velocity significantly exceeds user's baseline",
                details={
                    "current_count": recent_event_count,
                    "baseline_avg": baseline.avg_hourly_transactions,
                },
            )
        return None

    def _build_explanation(
        self, signals: list[Signal], severity: Severity, score: float
    ) -> str:
        if not signals:
            return "No anomalous signals detected."
        signal_names = [s.name.replace("_", " ") for s in signals]
        return (
            f"Risk score {score:.2f} ({severity.value}): "
            f"detected {', '.join(signal_names)}."
        )

    def compute_false_positive_rate(
        self,
        total_flags: int,
        cleared_flags: int,
    ) -> float:
        """Compute false-positive rate as ratio of cleared to total flags."""
        if total_flags == 0:
            return 0.0
        return cleared_flags / total_flags

    def self_clear_eligible(self, severity: Severity) -> bool:
        """Only medium-severity flags are eligible for user self-clear."""
        return severity == Severity.MEDIUM
