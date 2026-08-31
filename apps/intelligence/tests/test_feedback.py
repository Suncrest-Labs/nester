"""Tests for conversation rating and feedback capture (#926)."""

from unittest.mock import patch

from fastapi.testclient import TestClient

from app.main import app
from app.services.feedback_store import FeedbackEntryDict, _InMemoryFeedbackStore

client = TestClient(app)


# ---------------------------------------------------------------------------
# Feedback store unit tests
# ---------------------------------------------------------------------------


class TestInMemoryFeedbackStore:
    """Tests for the in-memory feedback store."""

    def test_submit_and_retrieve(self):
        store = _InMemoryFeedbackStore()
        entry: FeedbackEntryDict = {
            "id": "fb-1",
            "rating": "thumbs_up",
            "comment": "Great answer!",
            "conversation_id": "conv-1",
            "user_id": "user-1",
            "created_at": "2026-01-01T00:00:00Z",
        }
        store.submit("user-1", entry)
        results = store.get_all("user-1")
        assert len(results) == 1
        assert results[0]["rating"] == "thumbs_up"
        assert results[0]["comment"] == "Great answer!"

    def test_multiple_entries_per_user(self):
        store = _InMemoryFeedbackStore()
        store.submit("user-1", {
            "id": "fb-1",
            "rating": "thumbs_up",
            "comment": "",
            "conversation_id": "conv-1",
            "user_id": "user-1",
            "created_at": "2026-01-01T00:00:00Z",
        })
        store.submit("user-1", {
            "id": "fb-2",
            "rating": "thumbs_down",
            "comment": "Not helpful",
            "conversation_id": "conv-2",
            "user_id": "user-1",
            "created_at": "2026-01-02T00:00:00Z",
        })
        results = store.get_all("user-1")
        assert len(results) == 2

    def test_feedback_scoped_per_user(self):
        store = _InMemoryFeedbackStore()
        store.submit("user-1", {
            "id": "fb-1",
            "rating": "thumbs_up",
            "comment": "",
            "conversation_id": "conv-1",
            "user_id": "user-1",
            "created_at": "2026-01-01T00:00:00Z",
        })
        assert len(store.get_all("user-2")) == 0

    def test_get_all_returns_copy(self):
        store = _InMemoryFeedbackStore()
        store.submit("user-1", {
            "id": "fb-1",
            "rating": "thumbs_up",
            "comment": "",
            "conversation_id": "conv-1",
            "user_id": "user-1",
            "created_at": "2026-01-01T00:00:00Z",
        })
        data = store.get_all("user-1")
        # Mutating the returned list must not affect internal state
        data[0]["rating"] = "thumbs_down"
        original = store.get_all("user-1")
        assert original[0]["rating"] == "thumbs_up"

    def test_storage_capped(self):
        store = _InMemoryFeedbackStore()
        # Submit more than the max
        with patch(
            "app.services.feedback_store._MAX_FEEDBACK_PER_USER", 5
        ):
            for i in range(10):
                store.submit("user-1", {
                    "id": f"fb-{i}",
                    "rating": "thumbs_up",
                    "comment": "",
                    "conversation_id": "conv",
                    "user_id": "user-1",
                    "created_at": f"2026-01-{i+1:02d}T00:00:00Z",
                })
            results = store.get_all("user-1")
            assert len(results) == 5
            # Most recent entries should be kept
            assert results[-1]["id"] == "fb-9"

    def test_thumbs_down_stored_correctly(self):
        store = _InMemoryFeedbackStore()
        store.submit("user-1", {
            "id": "fb-1",
            "rating": "thumbs_down",
            "comment": "Wrong information",
            "conversation_id": "conv-1",
            "user_id": "user-1",
            "created_at": "2026-01-01T00:00:00Z",
        })
        results = store.get_all("user-1")
        assert results[0]["rating"] == "thumbs_down"


# ---------------------------------------------------------------------------
# Feedback API integration tests
# ---------------------------------------------------------------------------


class TestFeedbackAPI:
    """Tests for the feedback HTTP endpoints."""

    def test_submit_thumbs_up(self):
        """POST /intelligence/feedback should accept a thumbs_up rating."""
        response = client.post(
            "/intelligence/feedback",
            json={
                "rating": "thumbs_up",
                "comment": "Excellent response!",
                "conversation_id": "conv-abc-123",
            },
            params={"userId": "test-user-1"},
        )
        assert response.status_code == 201
        data = response.json()
        assert data["ok"] is True
        assert "feedback_id" in data

    def test_submit_thumbs_down(self):
        """POST /intelligence/feedback should accept a thumbs_down rating."""
        response = client.post(
            "/intelligence/feedback",
            json={
                "rating": "thumbs_down",
                "comment": "This was not accurate",
                "conversation_id": "conv-def-456",
            },
            params={"userId": "test-user-1"},
        )
        assert response.status_code == 201
        data = response.json()
        assert data["ok"] is True

    def test_submit_without_comment(self):
        """POST /intelligence/feedback should work without a comment."""
        response = client.post(
            "/intelligence/feedback",
            json={
                "rating": "thumbs_up",
                "conversation_id": "conv-xyz-789",
            },
            params={"userId": "test-user-2"},
        )
        assert response.status_code == 201
        data = response.json()
        assert data["ok"] is True

    def test_submit_invalid_rating(self):
        """POST /intelligence/feedback should reject invalid ratings."""
        response = client.post(
            "/intelligence/feedback",
            json={
                "rating": "invalid_rating",
                "conversation_id": "conv-1",
            },
            params={"userId": "test-user-1"},
        )
        assert response.status_code == 422  # Validation error

    def test_submit_comment_too_long(self):
        """POST /intelligence/feedback should reject oversized comments."""
        response = client.post(
            "/intelligence/feedback",
            json={
                "rating": "thumbs_up",
                "comment": "x" * 2001,  # max is 2000
                "conversation_id": "conv-1",
            },
            params={"userId": "test-user-1"},
        )
        assert response.status_code == 422  # Validation error

    def test_get_feedback_returns_entries(self):
        """GET /intelligence/feedback should return submitted feedback."""
        # Submit two feedback entries
        client.post(
            "/intelligence/feedback",
            json={"rating": "thumbs_up", "conversation_id": "conv-1"},
            params={"userId": "feedback-reader"},
        )
        client.post(
            "/intelligence/feedback",
            json={"rating": "thumbs_down", "comment": "Bad", "conversation_id": "conv-2"},
            params={"userId": "feedback-reader"},
        )

        response = client.get(
            "/intelligence/feedback",
            params={"userId": "feedback-reader"},
        )
        assert response.status_code == 200
        data = response.json()
        assert data["total"] >= 2
        # Verify structure
        entries = data["feedback_entries"]
        assert any(e["rating"] == "thumbs_up" for e in entries)
        assert any(e["rating"] == "thumbs_down" for e in entries)

    def test_get_feedback_empty(self):
        """GET /intelligence/feedback should return empty for a new user."""
        response = client.get(
            "/intelligence/feedback",
            params={"userId": "brand-new-user"},
        )
        assert response.status_code == 200
        data = response.json()
        assert data["total"] == 0
        assert data["feedback_entries"] == []

    def test_feedback_scoped_to_user(self):
        """Feedback from one user should not leak to another."""
        client.post(
            "/intelligence/feedback",
            json={"rating": "thumbs_up", "conversation_id": "conv-scoped"},
            params={"userId": "user-a"},
        )
        # User B should not see user A's feedback
        response = client.get(
            "/intelligence/feedback",
            params={"userId": "user-b"},
        )
        assert response.status_code == 200
        data = response.json()
        assert data["total"] == 0
