"""Unit tests for the market sentiment history tracker (#939)."""

import time

import pytest

from app.services import sentiment_history


@pytest.fixture(autouse=True)
def clear_history(monkeypatch):
    # Force the in-memory fallback path so tests don't depend on a real Redis
    # instance being reachable.
    monkeypatch.setattr(sentiment_history, "_get_redis", lambda: None)
    sentiment_history._mem_history.clear()
    yield
    sentiment_history._mem_history.clear()


def test_record_and_history_round_trip():
    # history() filters relative to the real clock, so observed_at values must
    # be recent (within the query window) for the point to be returned.
    now = time.time()
    sentiment_history.record("bull", 0.8, observed_at=now)

    points = sentiment_history.history(days=30)

    assert len(points) == 1
    assert points[0]["signal"] == "bull"
    assert points[0]["confidence"] == 0.8
    assert points[0]["observed_at"] == now


def test_history_returns_points_oldest_first():
    now = time.time()
    sentiment_history.record("bear", 0.5, observed_at=now - 30)
    sentiment_history.record("bull", 0.9, observed_at=now - 90)
    sentiment_history.record("neutral", 0.4, observed_at=now - 60)

    points = sentiment_history.history(days=30)

    assert [p["observed_at"] for p in points] == [now - 90, now - 60, now - 30]


def test_history_excludes_points_outside_window():
    day = 24 * 60 * 60
    now = time.time()
    sentiment_history.record("bull", 0.7, observed_at=now - 40 * day)  # older than a 30d window
    sentiment_history.record("bear", 0.6, observed_at=now - 5 * day)

    points = sentiment_history.history(days=30)

    assert len(points) == 1
    assert points[0]["signal"] == "bear"


def test_history_days_clamped_by_caller_not_by_history_itself():
    # history() itself trusts the days argument as given; clamping to [1, 30]
    # is the router's responsibility (tested via the endpoint).
    now = time.time()
    sentiment_history.record("bull", 0.7, observed_at=now - 1)

    assert len(sentiment_history.history(days=1)) == 1
    assert len(sentiment_history.history(days=7)) == 1


def test_record_prunes_entries_older_than_retention():
    now = 2_000_000.0
    stale = now - sentiment_history._RETENTION_SECONDS - 1
    sentiment_history.record("bull", 0.7, observed_at=stale)
    sentiment_history.record("bear", 0.6, observed_at=now)

    # The second record() call prunes anything older than the retention
    # window relative to its own observed_at.
    assert len(sentiment_history._mem_history) == 1
    assert sentiment_history._mem_history[0]["signal"] == "bear"


def test_history_empty_when_nothing_recorded():
    assert sentiment_history.history(days=7) == []
