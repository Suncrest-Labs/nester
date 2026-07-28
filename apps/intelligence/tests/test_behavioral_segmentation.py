from datetime import datetime, timedelta, timezone

from app.services.behavioral_segmentation import (
    ActivityEvent,
    BehavioralInput,
    BehavioralSegmentationService,
)


def _make_event(
    event_type: str,
    occurred_at: datetime,
    amount: float | None = None,
    vault_id: str | None = None,
) -> ActivityEvent:
    return ActivityEvent(
        event_type=event_type,
        occurred_at=occurred_at,
        amount=amount,
        vault_id=vault_id,
    )


def test_compute_profile_assigns_regular_saver_segment_from_real_activity() -> None:
    now = datetime(2026, 7, 27, tzinfo=timezone.utc)
    service = BehavioralSegmentationService()

    input_data = BehavioralInput(
        user_id="user-regular",
        transactions=[
            _make_event("deposit", now - timedelta(days=2), 100.0, "vault-a"),
            _make_event("deposit", now - timedelta(days=9), 100.0, "vault-a"),
            _make_event("deposit", now - timedelta(days=16), 100.0, "vault-a"),
            _make_event("deposit", now - timedelta(days=23), 100.0, "vault-a"),
            _make_event("deposit", now - timedelta(days=30), 100.0, "vault-a"),
        ],
        goals=[
            ActivityEvent(
                event_type="goal_completed",
                occurred_at=now - timedelta(days=10),
                amount=1000.0,
            ),
            ActivityEvent(
                event_type="goal_completed",
                occurred_at=now - timedelta(days=20),
                amount=500.0,
            ),
        ],
        streaks=[
            ActivityEvent(
                event_type="streak",
                occurred_at=now,
                metadata={"current_streak": 4, "longest_streak": 8},
            )
        ],
        engagement_events=[
            _make_event("engagement", now - timedelta(days=1)),
            _make_event("engagement", now - timedelta(days=4)),
            _make_event("engagement", now - timedelta(days=6)),
        ],
    )

    profile = service.segment_user(input_data)

    assert profile.features.contribution_regularity > 0.5
    assert profile.features.goal_completion_rate == 1.0
    assert profile.segment_code == "disciplined_regular_saver"
    assert profile.segment_label == "Disciplined regular saver"


def test_assigns_yield_optimizer_for_frequent_vault_switching() -> None:
    now = datetime(2026, 7, 27, tzinfo=timezone.utc)
    service = BehavioralSegmentationService()

    input_data = BehavioralInput(
        user_id="user-yield",
        transactions=[
            _make_event("deposit", now - timedelta(days=1), 40.0, "vault-a"),
            _make_event("vault_move", now - timedelta(days=3), 0.0, "vault-b"),
            _make_event("vault_move", now - timedelta(days=6), 0.0, "vault-c"),
            _make_event("vault_move", now - timedelta(days=10), 0.0, "vault-d"),
            _make_event("deposit", now - timedelta(days=12), 60.0, "vault-d"),
        ],
        goals=[],
        streaks=[],
        engagement_events=[_make_event("engagement", now - timedelta(days=2))],
    )

    profile = service.segment_user(input_data)

    assert profile.segment_code == "yield_optimizer"
    assert profile.segment_label == "Yield optimizer"


def test_hysteresis_prevents_single_day_flip_but_allows_sustained_change() -> None:
    now = datetime(2026, 7, 27, tzinfo=timezone.utc)
    service = BehavioralSegmentationService()

    regular_input = BehavioralInput(
        user_id="user-stable",
        transactions=[
            _make_event("deposit", now - timedelta(days=2), 100.0, "vault-a"),
            _make_event("deposit", now - timedelta(days=9), 100.0, "vault-a"),
            _make_event("deposit", now - timedelta(days=16), 100.0, "vault-a"),
            _make_event("deposit", now - timedelta(days=23), 100.0, "vault-a"),
        ],
        goals=[],
        streaks=[
            ActivityEvent(
                event_type="streak",
                occurred_at=now,
                metadata={"current_streak": 3, "longest_streak": 5},
            )
        ],
        engagement_events=[_make_event("engagement", now - timedelta(days=1))],
    )

    regular_profile = service.segment_user(regular_input)

    anomalous_input = BehavioralInput(
        user_id="user-stable",
        transactions=[
            _make_event("deposit", now - timedelta(days=2), 100.0, "vault-a"),
            _make_event("deposit", now - timedelta(days=9), 100.0, "vault-a"),
            _make_event("deposit", now - timedelta(days=16), 100.0, "vault-a"),
            _make_event("deposit", now - timedelta(days=23), 100.0, "vault-a"),
            _make_event("deposit", now - timedelta(days=60), 100.0, "vault-a"),
        ],
        goals=[],
        streaks=[
            ActivityEvent(
                event_type="streak",
                occurred_at=now,
                metadata={"current_streak": 1, "longest_streak": 5},
            )
        ],
        engagement_events=[_make_event("engagement", now - timedelta(days=1))],
    )

    anomalous_profile = service.segment_user(anomalous_input, previous_profile=regular_profile)
    assert anomalous_profile.segment_code == "disciplined_regular_saver"

    sustained_input = BehavioralInput(
        user_id="user-stable",
        transactions=[
            _make_event("deposit", now - timedelta(days=80), 100.0, "vault-a"),
            _make_event("deposit", now - timedelta(days=95), 100.0, "vault-a"),
            _make_event("deposit", now - timedelta(days=110), 100.0, "vault-a"),
            _make_event("deposit", now - timedelta(days=125), 100.0, "vault-a"),
        ],
        goals=[],
        streaks=[
            ActivityEvent(
                event_type="streak",
                occurred_at=now,
                metadata={"current_streak": 0, "longest_streak": 5},
            )
        ],
        engagement_events=[],
    )

    sustained_profile = service.segment_user(sustained_input, previous_profile=regular_profile)
    assert sustained_profile.segment_code == "dormant_at_risk"


def test_emits_transition_event_when_segment_changes() -> None:
    now = datetime(2026, 7, 27, tzinfo=timezone.utc)
    service = BehavioralSegmentationService()

    initial = service.segment_user(
        BehavioralInput(
            user_id="user-transitions",
            transactions=[_make_event("deposit", now - timedelta(days=2), 50.0, "vault-a")],
            goals=[],
            streaks=[
                ActivityEvent(
                    event_type="streak",
                    occurred_at=now,
                    metadata={"current_streak": 3, "longest_streak": 3},
                )
            ],
            engagement_events=[_make_event("engagement", now - timedelta(days=1))],
        )
    )

    changed = service.segment_user(
        BehavioralInput(
            user_id="user-transitions",
            transactions=[_make_event("deposit", now - timedelta(days=80), 50.0, "vault-a")],
            goals=[],
            streaks=[
                ActivityEvent(
                    event_type="streak",
                    occurred_at=now,
                    metadata={"current_streak": 0, "longest_streak": 3},
                )
            ],
            engagement_events=[],
        ),
        previous_profile=initial,
    )

    assert changed.transition is not None
    assert changed.transition["from"] == "disciplined_regular_saver"
    assert changed.transition["to"] == "dormant_at_risk"


def test_segmenting_ignores_protected_attributes_and_reports_analytics() -> None:
    now = datetime(2026, 7, 27, tzinfo=timezone.utc)
    service = BehavioralSegmentationService()

    profile = service.segment_user(
        BehavioralInput(
            user_id="user-fairness",
            transactions=[
                _make_event("deposit", now - timedelta(days=2), 20.0, "vault-a"),
                _make_event("deposit", now - timedelta(days=9), 20.0, "vault-a"),
            ],
            goals=[],
            streaks=[],
            engagement_events=[_make_event("engagement", now - timedelta(days=1))],
            protected_attributes={"age": 35, "gender": "female", "country": "US"},
        )
    )

    assert profile.segment_code == "new_exploring"
    analytics = service.summarize_segments([profile])
    assert analytics["new_exploring"] == 1
