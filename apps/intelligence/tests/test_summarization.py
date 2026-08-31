"""Tests for conversation history summarization (Issue 2 — Prometheus AI).

Covers:
- Token threshold detection (estimate_token_count, needs_summarization)
- Summarization trigger: history exceeding threshold causes summarize_history call
- New context construction: compacted history structure and audit log preservation
- Config defaults for max_history_tokens and history_recent_turns_kept
- ConversationStore set_active / get_audit round-trip (in-memory)
"""

from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from app.services.conversation_store import _InMemoryConversationStore
from app.services.summarization import (
    SUMMARY_PREFIX,
    _split_history,
    estimate_token_count,
    needs_summarization,
    summarize_history,
)

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def make_history(n: int, content_len: int = 100) -> list[dict[str, str]]:
    """Return a history list with ``n`` alternating user/assistant messages."""
    msgs = []
    for i in range(n):
        role = "user" if i % 2 == 0 else "assistant"
        msgs.append({"role": role, "content": "x" * content_len})
    return msgs


# ---------------------------------------------------------------------------
# estimate_token_count
# ---------------------------------------------------------------------------

class TestEstimateTokenCount:
    def test_empty_history_returns_zero(self):
        assert estimate_token_count([]) == 0

    def test_single_message_four_chars_per_token(self):
        history = [{"role": "user", "content": "abcd"}]
        assert estimate_token_count(history) == 1

    def test_multiple_messages_summed(self):
        history = [
            {"role": "user", "content": "a" * 400},
            {"role": "assistant", "content": "b" * 400},
        ]
        # 800 chars / 4 = 200 tokens
        assert estimate_token_count(history) == 200

    def test_empty_content_does_not_crash(self):
        history = [{"role": "user", "content": ""}]
        assert estimate_token_count(history) == 0

    def test_missing_content_key_treated_as_empty(self):
        history = [{"role": "user"}]  # type: ignore[typeddict-item]
        assert estimate_token_count(history) == 0


# ---------------------------------------------------------------------------
# needs_summarization
# ---------------------------------------------------------------------------

class TestNeedsSummarization:
    def test_empty_history_never_needs_summarization(self):
        assert needs_summarization([]) is False

    def test_below_threshold_returns_false(self):
        # 10 messages × 100 chars = 1000 chars → 250 tokens; threshold 80_000
        history = make_history(10, content_len=100)
        with patch("app.services.summarization.settings") as mock_settings:
            mock_settings.max_history_tokens = 80_000
            assert needs_summarization(history) is False

    def test_above_threshold_returns_true(self):
        # 10 messages × 100 chars = 1000 chars → 250 tokens; threshold 200
        history = make_history(10, content_len=100)
        with patch("app.services.summarization.settings") as mock_settings:
            mock_settings.max_history_tokens = 200
            assert needs_summarization(history) is True

    def test_exactly_at_threshold_returns_false(self):
        # exactly 200 tokens with threshold 200 — only *exceeding* triggers
        history = [{"role": "user", "content": "a" * 800}]
        with patch("app.services.summarization.settings") as mock_settings:
            mock_settings.max_history_tokens = 200
            assert needs_summarization(history) is False

    def test_one_above_threshold_returns_true(self):
        history = [{"role": "user", "content": "a" * 804}]  # 201 tokens
        with patch("app.services.summarization.settings") as mock_settings:
            mock_settings.max_history_tokens = 200
            assert needs_summarization(history) is True

    def test_disabled_when_threshold_zero(self):
        history = make_history(100, content_len=10_000)  # very large
        with patch("app.services.summarization.settings") as mock_settings:
            mock_settings.max_history_tokens = 0
            assert needs_summarization(history) is False


# ---------------------------------------------------------------------------
# _split_history
# ---------------------------------------------------------------------------

class TestSplitHistory:
    def test_splits_at_correct_position(self):
        history = make_history(10)
        older, recent = _split_history(history, recent_turns=4)
        assert len(older) == 6
        assert len(recent) == 4
        assert recent == history[6:]

    def test_minimum_recent_turns_is_two(self):
        history = make_history(10)
        older, recent = _split_history(history, recent_turns=1)
        assert len(recent) == 2

    def test_returns_all_recent_when_history_shorter_than_keep(self):
        history = make_history(3)
        older, recent = _split_history(history, recent_turns=4)
        assert older == []
        assert recent == history

    def test_empty_history_returns_empty_older(self):
        older, recent = _split_history([], recent_turns=4)
        assert older == []
        assert recent == []


# ---------------------------------------------------------------------------
# summarize_history
# ---------------------------------------------------------------------------

class TestSummarizeHistory:
    @pytest.mark.asyncio
    async def test_returns_compacted_history_on_success(self):
        history = make_history(10, content_len=100)

        mock_block = MagicMock()
        mock_block.text = "User discussed yield strategies and chose the Balanced vault."
        mock_response = MagicMock()
        mock_response.content = [mock_block]

        mock_client = AsyncMock()
        mock_client.messages.create = AsyncMock(return_value=mock_response)

        with patch("app.services.summarization.settings") as mock_settings:
            mock_settings.anthropic_model = "claude-sonnet-5"
            mock_settings.history_recent_turns_kept = 4

            compacted = await summarize_history(history, mock_client)

        # First message should be the summary sentinel
        assert len(compacted) == 5  # 1 summary + 4 recent
        assert compacted[0]["role"] == "assistant"
        assert compacted[0]["content"].startswith(SUMMARY_PREFIX)
        assert "yield strategies" in compacted[0]["content"]

        # Last 4 messages should be the original recent turns verbatim
        assert compacted[1:] == history[6:]

    @pytest.mark.asyncio
    async def test_returns_original_history_on_api_error(self):
        history = make_history(10, content_len=100)

        mock_client = AsyncMock()
        mock_client.messages.create = AsyncMock(side_effect=Exception("API unavailable"))

        with patch("app.services.summarization.settings") as mock_settings:
            mock_settings.anthropic_model = "claude-sonnet-5"
            mock_settings.history_recent_turns_kept = 4

            result = await summarize_history(history, mock_client)

        # On failure, original history is returned unchanged
        assert result == history

    @pytest.mark.asyncio
    async def test_returns_original_history_on_empty_summary(self):
        history = make_history(10, content_len=100)

        mock_block = MagicMock()
        mock_block.text = "   "  # empty/whitespace only
        mock_response = MagicMock()
        mock_response.content = [mock_block]

        mock_client = AsyncMock()
        mock_client.messages.create = AsyncMock(return_value=mock_response)

        with patch("app.services.summarization.settings") as mock_settings:
            mock_settings.anthropic_model = "claude-sonnet-5"
            mock_settings.history_recent_turns_kept = 4

            result = await summarize_history(history, mock_client)

        assert result == history

    @pytest.mark.asyncio
    async def test_nothing_to_summarize_returns_original(self):
        """History shorter than keep window is returned unchanged."""
        history = make_history(3)

        mock_client = AsyncMock()

        with patch("app.services.summarization.settings") as mock_settings:
            mock_settings.anthropic_model = "claude-sonnet-5"
            mock_settings.history_recent_turns_kept = 6  # keep more than history length

            result = await summarize_history(history, mock_client)

        assert result == history
        mock_client.messages.create.assert_not_called()

    @pytest.mark.asyncio
    async def test_compacted_history_token_count_is_lower(self):
        """Token estimate of compacted history should be less than original."""
        # 20 messages × 500 chars each → 2500 tokens
        history = make_history(20, content_len=500)

        summary_text = "Brief summary."  # 14 chars → ~3 tokens
        mock_block = MagicMock()
        mock_block.text = summary_text
        mock_response = MagicMock()
        mock_response.content = [mock_block]

        mock_client = AsyncMock()
        mock_client.messages.create = AsyncMock(return_value=mock_response)

        with patch("app.services.summarization.settings") as mock_settings:
            mock_settings.anthropic_model = "claude-sonnet-5"
            mock_settings.history_recent_turns_kept = 4

            compacted = await summarize_history(history, mock_client)

        original_tokens = estimate_token_count(history)
        compacted_tokens = estimate_token_count(compacted)
        assert compacted_tokens < original_tokens


# ---------------------------------------------------------------------------
# InMemoryConversationStore — set_active / get_audit
# ---------------------------------------------------------------------------

class TestInMemoryConversationStoreAudit:
    def test_append_writes_to_audit(self):
        store = _InMemoryConversationStore()
        store.append("user-1", "user", "hello")
        store.append("user-1", "assistant", "hi there")

        audit = store.get_audit("user-1")
        assert len(audit) == 2
        assert audit[0] == {"role": "user", "content": "hello"}
        assert audit[1] == {"role": "assistant", "content": "hi there"}

    def test_set_active_replaces_active_history(self):
        store = _InMemoryConversationStore()
        store.append("user-1", "user", "original message")

        compacted = [{"role": "assistant", "content": f"{SUMMARY_PREFIX}Summary here."}]
        store.set_active("user-1", compacted)

        active = store.get("user-1")
        assert active == compacted

    def test_audit_not_affected_by_set_active(self):
        store = _InMemoryConversationStore()
        store.append("user-1", "user", "original message")

        compacted = [{"role": "assistant", "content": f"{SUMMARY_PREFIX}Summary here."}]
        store.set_active("user-1", compacted)

        # Audit still has the original message
        audit = store.get_audit("user-1")
        assert any(m["content"] == "original message" for m in audit)

    def test_clear_removes_active_not_audit(self):
        store = _InMemoryConversationStore()
        store.append("user-1", "user", "message before clear")
        store.clear("user-1")

        assert store.get("user-1") == []
        # Audit survives clear
        audit = store.get_audit("user-1")
        assert len(audit) == 1

    def test_audit_scoped_per_user(self):
        store = _InMemoryConversationStore()
        store.append("user-1", "user", "user-1 message")
        store.append("user-2", "user", "user-2 message")

        assert len(store.get_audit("user-1")) == 1
        assert len(store.get_audit("user-2")) == 1
        assert store.get_audit("user-99") == []

    def test_get_audit_returns_copy(self):
        store = _InMemoryConversationStore()
        store.append("user-1", "user", "original")

        audit = store.get_audit("user-1")
        audit.append({"role": "user", "content": "mutated"})

        # Internal audit must not have been mutated
        assert len(store.get_audit("user-1")) == 1


# ---------------------------------------------------------------------------
# Config defaults
# ---------------------------------------------------------------------------

class TestConfigDefaults:
    def test_max_history_tokens_default(self):
        from app.config import Settings

        s = Settings()
        assert s.max_history_tokens == 80_000

    def test_history_recent_turns_kept_default(self):
        from app.config import Settings

        s = Settings()
        assert s.history_recent_turns_kept == 6

    def test_max_history_tokens_configurable(self):
        import os

        from app.config import Settings

        with patch.dict(os.environ, {"INTELLIGENCE_MAX_HISTORY_TOKENS": "40000"}, clear=False):
            s = Settings()
            assert s.max_history_tokens == 40_000

    def test_history_recent_turns_kept_configurable(self):
        import os

        from app.config import Settings

        with patch.dict(
            os.environ, {"INTELLIGENCE_HISTORY_RECENT_TURNS_KEPT": "10"}, clear=False
        ):
            s = Settings()
            assert s.history_recent_turns_kept == 10
