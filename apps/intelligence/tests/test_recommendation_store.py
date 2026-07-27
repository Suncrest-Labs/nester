"""Tests for the dismissed/acted-on recommendation engagement store (#847)."""

from app.services.recommendation_store import _InMemoryEngagementStore


class TestInMemoryEngagementStore:
    def test_dismiss_then_is_dismissed(self):
        store = _InMemoryEngagementStore()
        store.record("user-1", "increase_contribution:goal-1", "dismissed")
        assert store.is_dismissed("user-1", "increase_contribution:goal-1") is True

    def test_unrecorded_candidate_is_not_dismissed(self):
        store = _InMemoryEngagementStore()
        assert store.is_dismissed("user-1", "increase_contribution:goal-1") is False

    def test_acted_on_is_not_treated_as_dismissed(self):
        store = _InMemoryEngagementStore()
        store.record("user-1", "move_to_higher_yield:vault-1", "acted_on")
        assert store.is_dismissed("user-1", "move_to_higher_yield:vault-1") is False
        assert store.get_all("user-1")["move_to_higher_yield:vault-1"]["engagement"] == "acted_on"

    def test_engagement_scoped_per_user(self):
        store = _InMemoryEngagementStore()
        store.record("user-1", "increase_contribution:goal-1", "dismissed")
        assert store.is_dismissed("user-2", "increase_contribution:goal-1") is False

    def test_get_all_returns_copy(self):
        store = _InMemoryEngagementStore()
        store.record("user-1", "increase_contribution:goal-1", "dismissed")
        data = store.get_all("user-1")
        data["increase_contribution:goal-1"]["engagement"] = "acted_on"
        # Mutating the returned dict must not affect the store's internal state.
        assert store.is_dismissed("user-1", "increase_contribution:goal-1") is True
