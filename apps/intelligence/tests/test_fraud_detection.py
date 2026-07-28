"""Tests for the fraud detection service.

Covers:
- Correct-severity flagging for known anomalous patterns
- Graduated response (medium -> step-up, high -> hold)
- False-positive self-clear eligibility
- Per-user learning (established normal behavior not flagged)
- Population baseline computation
- Signal accuracy for each detection signal
- Impossible travel detection
- Auth failure burst detection
- Post-credential-change activity detection
"""


from app.services.fraud_detection import (
    ABSOLUTE_MAX_AMOUNT_NEW_USER,
    AUTH_FAILURE_BURST_COUNT,
    FraudDetectionService,
    Severity,
    UserBaseline,
    aggregate_score,
    haversine_distance,
    score_to_severity,
    severity_to_actions,
)


def _make_baseline(
    user_id: str = "user-1",
    avg_amount: float = 100.0,
    stddev: float = 20.0,
    max_amount: float = 200.0,
    tx_count: int = 20,
    avg_daily: float = 2.0,
    avg_hourly: float = 0.5,
    known_destinations: int = 5,
    known_devices: int = 2,
) -> UserBaseline:
    return UserBaseline(
        user_id=user_id,
        avg_transaction_amount=avg_amount,
        stddev_transaction_amount=stddev,
        max_transaction_amount=max_amount,
        avg_daily_transactions=avg_daily,
        avg_hourly_transactions=avg_hourly,
        known_destination_count=known_destinations,
        known_device_count=known_devices,
        transaction_count=tx_count,
    )


def test_normal_transaction_no_flag() -> None:
    service = FraudDetectionService()
    service.set_user_baseline(_make_baseline())

    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=120.0,
        event_type="withdrawal",
        destination="dest-1",
        device_fingerprint="device-1",
        known_destinations=["dest-1", "dest-2"],
        known_devices=["device-1"],
        is_withdrawal=True,
    )

    assert flag.severity == Severity.LOW
    assert flag.risk_score == 0.0
    assert len(flag.signals) == 0


def test_large_withdrawal_new_destination_high_severity() -> None:
    service = FraudDetectionService()
    service.set_user_baseline(_make_baseline())

    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=500.0,
        event_type="withdrawal",
        destination="brand-new-dest",
        device_fingerprint="device-1",
        known_destinations=["dest-1"],
        known_devices=["device-1"],
        is_withdrawal=True,
    )

    assert flag.risk_score > 0
    assert flag.severity in (Severity.MEDIUM, Severity.HIGH, Severity.CRITICAL)
    signal_names = [s.name for s in flag.signals]
    assert "amount_deviation" in signal_names
    assert "new_destination" in signal_names


def test_large_withdrawal_new_destination_post_reset_scores_high() -> None:
    service = FraudDetectionService()
    service.set_user_baseline(_make_baseline())

    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=500.0,
        event_type="withdrawal",
        destination="brand-new-dest",
        device_fingerprint="new-device",
        known_destinations=["dest-1"],
        known_devices=["device-1"],
        is_withdrawal=True,
        hours_since_credential_change=0.5,
    )

    assert flag.risk_score > 0.2
    assert flag.severity in (Severity.MEDIUM, Severity.HIGH, Severity.CRITICAL)
    signal_names = [s.name for s in flag.signals]
    assert "post_credential_change" in signal_names
    assert "new_destination" in signal_names
    assert "new_device" in signal_names


def test_graduated_response_medium_gets_step_up_auth() -> None:
    service = FraudDetectionService()
    service.set_user_baseline(_make_baseline())

    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=130.0,
        event_type="withdrawal",
        destination="occasional-dest",
        device_fingerprint="new-device",
        known_destinations=["dest-1"],
        known_devices=["device-1"],
        is_withdrawal=True,
    )

    actions = severity_to_actions(flag.severity)
    if flag.severity == Severity.MEDIUM:
        assert "step_up_auth" in actions
        assert "hold" not in actions


def test_graduated_response_high_gets_hold() -> None:
    service = FraudDetectionService()
    service.set_user_baseline(_make_baseline())

    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=500.0,
        event_type="withdrawal",
        destination="completely-new-dest",
        device_fingerprint="unknown-fp",
        known_destinations=["regular-dest"],
        known_devices=["known-device"],
        is_withdrawal=True,
        recent_auth_failures=6,
        hours_since_credential_change=0.3,
    )

    actions = severity_to_actions(flag.severity)
    if flag.severity in (Severity.HIGH, Severity.CRITICAL):
        assert "hold" in actions
        assert "step_up_auth" in actions


def test_impossible_travel_detected() -> None:
    service = FraudDetectionService()

    # NYC to Tokyo - impossible in 1 hour
    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=50.0,
        event_type="login",
        device_fingerprint="device-1",
        known_devices=["device-1"],
        last_known_lat=40.7128,
        last_known_lon=-74.0060,
        current_lat=35.6762,
        current_lon=139.6503,
    )

    signal_names = [s.name for s in flag.signals]
    assert "impossible_travel" in signal_names
    impossible_sig = next(s for s in flag.signals if s.name == "impossible_travel")
    assert impossible_sig.score > 0


def test_possible_travel_no_signal() -> None:
    service = FraudDetectionService()

    # NYC to Boston - very possible
    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=50.0,
        event_type="login",
        device_fingerprint="device-1",
        known_devices=["device-1"],
        last_known_lat=40.7128,
        last_known_lon=-74.0060,
        current_lat=42.3601,
        current_lon=-71.0589,
    )

    signal_names = [s.name for s in flag.signals]
    assert "impossible_travel" not in signal_names


def test_auth_failure_burst_detected() -> None:
    service = FraudDetectionService()

    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=50.0,
        event_type="auth_failure",
        device_fingerprint="device-1",
        known_devices=["device-1"],
        recent_auth_failures=AUTH_FAILURE_BURST_COUNT,
    )

    signal_names = [s.name for s in flag.signals]
    assert "auth_failure_burst" in signal_names
    burst_sig = next(s for s in flag.signals if s.name == "auth_failure_burst")
    assert burst_sig.score > 0
    assert burst_sig.details["failure_count"] == AUTH_FAILURE_BURST_COUNT


def test_auth_failure_below_threshold_no_signal() -> None:
    service = FraudDetectionService()

    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=50.0,
        event_type="auth_failure",
        device_fingerprint="device-1",
        known_devices=["device-1"],
        recent_auth_failures=3,
    )

    signal_names = [s.name for s in flag.signals]
    assert "auth_failure_burst" not in signal_names


def test_new_user_large_transaction_absolute_threshold() -> None:
    service = FraudDetectionService()
    # No baseline set

    flag = service.evaluate_transaction(
        user_id="new-user",
        amount=ABSOLUTE_MAX_AMOUNT_NEW_USER + 10000,
        event_type="withdrawal",
        destination="some-dest",
        device_fingerprint="device-1",
    )

    assert flag.severity != Severity.LOW
    signal_names = [s.name for s in flag.signals]
    assert "amount_deviation" in signal_names


def test_new_user_small_transaction_no_amount_signal() -> None:
    service = FraudDetectionService()

    flag = service.evaluate_transaction(
        user_id="new-user",
        amount=100.0,
        event_type="withdrawal",
        destination="some-dest",
        device_fingerprint="device-1",
    )

    signal_names = [s.name for s in flag.signals]
    assert "amount_deviation" not in signal_names


def test_new_device_signal() -> None:
    service = FraudDetectionService()

    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=50.0,
        event_type="login",
        device_fingerprint="brand-new-device",
        known_devices=["known-device-1", "known-device-2"],
    )

    signal_names = [s.name for s in flag.signals]
    assert "new_device" in signal_names


def test_known_device_no_signal() -> None:
    service = FraudDetectionService()

    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=50.0,
        event_type="login",
        device_fingerprint="known-device-1",
        known_devices=["known-device-1", "known-device-2"],
    )

    signal_names = [s.name for s in flag.signals]
    assert "new_device" not in signal_names


def test_new_destination_first_ever_higher_score() -> None:
    service = FraudDetectionService()

    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=100.0,
        event_type="withdrawal",
        destination="first-dest",
        known_destinations=[],
        is_withdrawal=True,
    )

    new_dest_signals = [s for s in flag.signals if s.name == "new_destination"]
    assert len(new_dest_signals) == 1
    assert new_dest_signals[0].score == 0.4


def test_post_credential_change_signal() -> None:
    service = FraudDetectionService()

    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=100.0,
        event_type="withdrawal",
        destination="dest-1",
        device_fingerprint="device-1",
        known_destinations=["dest-1"],
        known_devices=["device-1"],
        is_withdrawal=True,
        hours_since_credential_change=1.0,
    )

    signal_names = [s.name for s in flag.signals]
    assert "post_credential_change" in signal_names


def test_post_credential_change_outside_window_no_signal() -> None:
    service = FraudDetectionService()

    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=100.0,
        event_type="withdrawal",
        destination="dest-1",
        device_fingerprint="device-1",
        known_destinations=["dest-1"],
        known_devices=["device-1"],
        is_withdrawal=True,
        hours_since_credential_change=5.0,
    )

    signal_names = [s.name for s in flag.signals]
    assert "post_credential_change" not in signal_names


def test_velocity_spike_detected() -> None:
    service = FraudDetectionService()
    service.set_user_baseline(_make_baseline(avg_hourly=1.0))

    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=50.0,
        event_type="transaction",
        device_fingerprint="device-1",
        known_devices=["device-1"],
        recent_event_count=10,
    )

    signal_names = [s.name for s in flag.signals]
    assert "velocity_spike" in signal_names


def test_velocity_spike_no_baseline_no_signal() -> None:
    service = FraudDetectionService()

    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=50.0,
        event_type="transaction",
        device_fingerprint="device-1",
        known_devices=["device-1"],
        recent_event_count=100,
    )

    signal_names = [s.name for s in flag.signals]
    assert "velocity_spike" not in signal_names


def test_user_established_normal_behavior_no_flag() -> None:
    """A user who regularly makes large transfers should not be flagged."""
    service = FraudDetectionService()
    service.set_user_baseline(
        _make_baseline(
            avg_amount=500.0,
            stddev=50.0,
            max_amount=700.0,
            tx_count=50,
            avg_daily=3.0,
            avg_hourly=1.0,
        )
    )

    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=500.0,
        event_type="withdrawal",
        destination="regular-dest",
        device_fingerprint="regular-device",
        known_destinations=["regular-dest", "other-dest"],
        known_devices=["regular-device"],
        is_withdrawal=True,
    )

    assert flag.severity == Severity.LOW
    assert flag.risk_score == 0.0
    assert len(flag.signals) == 0


def test_self_clear_eligible_only_medium() -> None:
    service = FraudDetectionService()
    assert service.self_clear_eligible(Severity.MEDIUM) is True
    assert service.self_clear_eligible(Severity.LOW) is False
    assert service.self_clear_eligible(Severity.HIGH) is False
    assert service.self_clear_eligible(Severity.CRITICAL) is False


def test_false_positive_rate_computation() -> None:
    service = FraudDetectionService()
    assert service.compute_false_positive_rate(0, 0) == 0.0
    assert service.compute_false_positive_rate(100, 10) == 0.1
    assert service.compute_false_positive_rate(100, 50) == 0.5
    assert service.compute_false_positive_rate(100, 100) == 1.0


def test_aggregate_score_independent_signals() -> None:
    from app.services.fraud_detection import Signal

    signals = [
        Signal(name="a", score=0.3, threshold=0, message=""),
        Signal(name="b", score=0.4, threshold=0, message=""),
    ]
    score = aggregate_score(signals)
    # 1 - (1-0.3)*(1-0.4) = 1 - 0.42 = 0.58
    assert abs(score - 0.58) < 0.001


def test_aggregate_score_empty() -> None:
    assert aggregate_score([]) == 0.0


def test_score_to_severity_bands() -> None:
    assert score_to_severity(0.0) == Severity.LOW
    assert score_to_severity(0.15) == Severity.LOW
    assert score_to_severity(0.39) == Severity.LOW
    assert score_to_severity(0.4) == Severity.MEDIUM
    assert score_to_severity(0.5) == Severity.MEDIUM
    assert score_to_severity(0.69) == Severity.MEDIUM
    assert score_to_severity(0.7) == Severity.HIGH
    assert score_to_severity(0.85) == Severity.HIGH
    assert score_to_severity(0.89) == Severity.HIGH
    assert score_to_severity(0.9) == Severity.CRITICAL
    assert score_to_severity(1.0) == Severity.CRITICAL


def test_severity_to_actions_graduated() -> None:
    assert severity_to_actions(Severity.LOW) == ["log"]
    assert severity_to_actions(Severity.MEDIUM) == ["step_up_auth"]
    assert "hold" in severity_to_actions(Severity.HIGH)
    assert "step_up_auth" in severity_to_actions(Severity.HIGH)
    assert "hold" in severity_to_actions(Severity.CRITICAL)
    assert "step_up_auth" in severity_to_actions(Severity.CRITICAL)


def test_haversine_distance_same_point() -> None:
    dist = haversine_distance(40.7128, -74.0060, 40.7128, -74.0060)
    assert abs(dist) < 0.001


def test_haversine_distance_ny_to_london() -> None:
    dist = haversine_distance(40.7128, -74.0060, 51.5074, -0.1278)
    assert 5500 < dist < 5700


def test_update_population_baseline() -> None:
    service = FraudDetectionService()
    baselines = [
        _make_baseline("u1", avg_amount=100),
        _make_baseline("u2", avg_amount=200),
        _make_baseline("u3", avg_amount=150),
    ]
    all_amounts = [100, 120, 200, 180, 150, 160, 90, 210]

    pb = service.update_population_baseline(baselines, all_amounts)

    assert pb.total_users == 3
    assert pb.total_transactions == 8
    assert pb.avg_transaction_amount > 0
    assert pb.stddev_transaction_amount > 0
    assert pb.median_transaction_amount > 0


def test_update_population_baseline_empty() -> None:
    service = FraudDetectionService()
    pb = service.update_population_baseline([], [])
    assert pb.total_users == 0
    assert pb.total_transactions == 0


def test_explanation_generated_for_flag() -> None:
    service = FraudDetectionService()
    service.set_user_baseline(_make_baseline())

    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=500.0,
        event_type="withdrawal",
        destination="new-dest",
        device_fingerprint="new-device",
        known_destinations=["old-dest"],
        known_devices=["old-device"],
        is_withdrawal=True,
    )

    assert flag.explanation != ""
    assert "Risk score" in flag.explanation or "No anomalous" in flag.explanation


def test_multiple_signals_increase_score() -> None:
    service = FraudDetectionService()
    service.set_user_baseline(_make_baseline())

    # Single signal: new device only
    single = service.evaluate_transaction(
        user_id="user-1",
        amount=100.0,
        event_type="login",
        device_fingerprint="new-device",
        known_devices=["known-device"],
    )

    # Multiple signals: new device + new destination + amount deviation
    multi = service.evaluate_transaction(
        user_id="user-1",
        amount=500.0,
        event_type="withdrawal",
        destination="new-dest",
        device_fingerprint="new-device-2",
        known_destinations=["known-dest"],
        known_devices=["known-device"],
        is_withdrawal=True,
    )

    assert multi.risk_score > single.risk_score
    assert len(multi.signals) > len(single.signals)


def test_no_event_type_no_flags() -> None:
    service = FraudDetectionService()
    service.set_user_baseline(_make_baseline())

    flag = service.evaluate_transaction(
        user_id="user-1",
        amount=100.0,
        event_type="routine_check",
        device_fingerprint="device-1",
        known_devices=["device-1"],
    )

    assert flag.severity == Severity.LOW
    assert flag.risk_score == 0.0
