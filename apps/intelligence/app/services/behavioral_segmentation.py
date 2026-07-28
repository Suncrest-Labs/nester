from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any


@dataclass(slots=True)
class ActivityEvent:
    """A lightweight activity event derived from the platform's own activity stream."""

    event_type: str
    occurred_at: datetime
    amount: float | None = None
    vault_id: str | None = None
    metadata: dict[str, Any] = field(default_factory=dict)


@dataclass(slots=True)
class BehavioralInput:
    """Behavioral signals available for a single user."""

    user_id: str
    transactions: list[ActivityEvent]
    goals: list[ActivityEvent]
    streaks: list[ActivityEvent]
    engagement_events: list[ActivityEvent]
    protected_attributes: dict[str, Any] | None = None


@dataclass(slots=True)
class BehavioralFeatures:
    contribution_regularity: float
    goal_completion_rate: float
    yield_engagement: float
    tenure_days: int
    recency_days: int
    average_deposit_size: float
    deposit_size_variability: float
    streak_strength: float
    engagement_score: float
    move_frequency: float
    dormant_score: float


@dataclass(slots=True)
class BehavioralProfile:
    user_id: str
    features: BehavioralFeatures
    segment_code: str
    segment_label: str
    transition: dict[str, Any] | None = None


class BehavioralSegmentationService:
    """Assign interpretable behavioral segments from deterministic user activity signals.

    The implementation intentionally uses explicit rules rather than opaque clustering.
    This makes the segments explainable to product and engineering teams while staying
    grounded in real platform activity such as deposits, vault moves, goals, streaks,
    and engagement events.
    """

    def __init__(self) -> None:
        self._segment_descriptors = {
            "disciplined_regular_saver": "Disciplined regular saver",
            "aspirational_goal_setter": "Aspiration goal setter",
            "yield_optimizer": "Yield optimizer",
            "dormant_at_risk": "Dormant / at risk",
            "new_exploring": "New / exploring",
        }

    def segment_user(
        self,
        input_data: BehavioralInput,
        previous_profile: BehavioralProfile | None = None,
    ) -> BehavioralProfile:
        features = self._compute_features(input_data)
        segment_code, segment_label = self._assign_segment(features, previous_profile)
        transition = None

        if previous_profile is not None and previous_profile.segment_code != segment_code:
            transition = {
                "from": previous_profile.segment_code,
                "to": segment_code,
                "reason": self._transition_reason(features, previous_profile.features),
            }

        return BehavioralProfile(
            user_id=input_data.user_id,
            features=features,
            segment_code=segment_code,
            segment_label=segment_label,
            transition=transition,
        )

    def summarize_segments(self, profiles: list[BehavioralProfile]) -> dict[str, int]:
        counts: dict[str, int] = {
            code: 0 for code in self._segment_descriptors
        }
        for profile in profiles:
            counts[profile.segment_code] += 1
        return counts

    def _compute_features(self, input_data: BehavioralInput) -> BehavioralFeatures:
        all_events = (
            input_data.transactions + input_data.goals
            + input_data.streaks + input_data.engagement_events
        )
        reference_times = [
            event.occurred_at
            for event in all_events
            if event.occurred_at is not None
        ]
        now = max(reference_times, default=datetime.now(timezone.utc))
        deposits = [
            e for e in input_data.transactions
            if e.event_type == "deposit"
        ]
        vault_moves = [
            e for e in input_data.transactions
            if e.event_type == "vault_move"
        ]
        completed_goals = [
            e for e in input_data.goals
            if e.event_type == "goal_completed"
        ]

        if deposits:
            deposit_amounts = [max(event.amount or 0.0, 0.0) for event in deposits]
            avg_deposit = sum(deposit_amounts) / len(deposit_amounts)
            mean = avg_deposit
            variance = sum(
                (amount - mean) ** 2 for amount in deposit_amounts
            ) / len(deposit_amounts)
            variability = variance ** 0.5 / (avg_deposit or 1.0)
            regularity = self._compute_regularity(deposits, now)
        else:
            avg_deposit = 0.0
            variability = 0.0
            regularity = 0.0

        goal_completion_rate = len(completed_goals) / max(len(input_data.goals), 1)
        if input_data.goals:
            goal_completion_rate = len(completed_goals) / len(input_data.goals)
        else:
            goal_completion_rate = 0.0

        yield_engagement = min(1.0, len(vault_moves) / 3.0)
        move_frequency = min(1.0, len(vault_moves) / 4.0)
        engagement_score = min(1.0, len(input_data.engagement_events) / 3.0)
        streak_strength = 0.0
        if input_data.streaks:
            streak = input_data.streaks[-1]
            current_streak = int(streak.metadata.get("current_streak", 0) or 0)
            longest_streak = int(streak.metadata.get("longest_streak", 0) or 0)
            streak_strength = min(1.0, (current_streak / 7.0) + (longest_streak / 30.0) / 2.0)

        deposit_events = deposits or []
        first_deposit = min(
            (e.occurred_at for e in deposit_events), default=now
        )
        tenure_days = max(0, int((now - first_deposit).days))
        recency_days = 0
        if deposits:
            last_deposit = max(e.occurred_at for e in deposits)
            recency_days = max(0, int((now - last_deposit).days))
        elif input_data.engagement_events:
            last_event = max(
                e.occurred_at for e in input_data.engagement_events
            )
            recency_days = max(0, int((now - last_event).days))
        else:
            recency_days = 999

        dormant_score = max(0.0, 1.0 - min(1.0, regularity + engagement_score * 0.5))
        if recency_days > 45:
            dormant_score = max(dormant_score, min(1.0, recency_days / 90.0))

        return BehavioralFeatures(
            contribution_regularity=regularity,
            goal_completion_rate=goal_completion_rate,
            yield_engagement=yield_engagement,
            tenure_days=tenure_days,
            recency_days=recency_days,
            average_deposit_size=avg_deposit,
            deposit_size_variability=variability,
            streak_strength=streak_strength,
            engagement_score=engagement_score,
            move_frequency=move_frequency,
            dormant_score=dormant_score,
        )

    def _assign_segment(
        self,
        features: BehavioralFeatures,
        previous_profile: BehavioralProfile | None,
    ) -> tuple[str, str]:
        if features.move_frequency >= 0.6 and features.yield_engagement >= 0.4:
            return "yield_optimizer", self._segment_descriptors["yield_optimizer"]

        if (
            features.contribution_regularity >= 0.35
            and features.streak_strength >= 0.25
            and features.engagement_score >= 0.25
        ):
            return (
                "disciplined_regular_saver",
                self._segment_descriptors["disciplined_regular_saver"],
            )

        if (
            features.goal_completion_rate >= 0.5
            and features.engagement_score >= 0.2
        ):
            return (
                "aspirational_goal_setter",
                self._segment_descriptors["aspirational_goal_setter"],
            )

        if (
            previous_profile is not None
            and previous_profile.segment_code == "disciplined_regular_saver"
        ):
            if (
                features.contribution_regularity >= 0.35
                and features.streak_strength >= 0.12
                and features.engagement_score >= 0.15
            ):
                code = previous_profile.segment_code
                desc = self._segment_descriptors[code]
                return code, desc

        if features.dormant_score >= 0.6 or features.recency_days >= 45:
            return "dormant_at_risk", self._segment_descriptors["dormant_at_risk"]

        return "new_exploring", self._segment_descriptors["new_exploring"]

    def _compute_regularity(self, deposits: list[ActivityEvent], now: datetime) -> float:
        if not deposits:
            return 0.0
        sorted_events = sorted(deposits, key=lambda item: item.occurred_at)
        if len(sorted_events) < 2:
            return 0.35
        intervals: list[int] = []
        for index in range(1, len(sorted_events)):
            prev = sorted_events[index - 1].occurred_at
            curr = sorted_events[index].occurred_at
            delta_days = max(1, int((curr - prev).days))
            intervals.append(delta_days)
        median_interval = sorted(intervals)[len(intervals) // 2] if len(intervals) % 2 == 1 else (
            sorted(intervals)[len(intervals) // 2 - 1] + sorted(intervals)[len(intervals) // 2]
        ) / 2
        if median_interval <= 14:
            return 0.8
        if median_interval <= 30:
            return 0.6
        return 0.35

    def _transition_reason(
        self,
        features: BehavioralFeatures,
        previous_features: BehavioralFeatures,
    ) -> str:
        if features.dormant_score >= 0.6:
            return "dormancy_signal"
        if features.move_frequency > previous_features.move_frequency:
            return "yield_behavior_shift"
        return "behavioral_shift"
