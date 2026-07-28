"""Tests for stream_chat's handling of Claude API rate-limit/overload errors (#928).

Confirms that a 429 (rate-limited) or 529 (overloaded) response from Claude
surfaces as a distinct, friendly SSE message rather than being lumped in
with the generic "trouble connecting" catch-all, or propagating as a raw
500 to the caller.
"""

from types import SimpleNamespace
from typing import Any

import anthropic
import httpx
import pytest

from app.services import prometheus
from app.services.retrieval import RetrievedContext


class RaisingStream:
    """Fake `client.messages.stream(...)` context manager that raises on entry."""

    def __init__(self, exc: Exception) -> None:
        self._exc = exc

    async def __aenter__(self) -> "RaisingStream":
        raise self._exc

    async def __aexit__(self, *args: Any) -> None:
        return None


class FakeMessages:
    def __init__(self, exc: Exception) -> None:
        self._exc = exc

    def stream(self, **kwargs: Any) -> RaisingStream:
        return RaisingStream(self._exc)


class FakeClient:
    def __init__(self, exc: Exception) -> None:
        self.messages = FakeMessages(exc)


class FakeRetrievalService:
    async def retrieve(self, user_id: str, message: str) -> RetrievedContext:
        return RetrievedContext()


def _make_status_error(status_code: int) -> anthropic.APIStatusError:
    response = httpx.Response(status_code, request=httpx.Request("POST", "http://x"))
    return anthropic.APIStatusError("boom", response=response, body=None)


async def _collect(agen: Any) -> list[str]:
    return [chunk async for chunk in agen]


@pytest.fixture(autouse=True)
def _stub_dependencies(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(prometheus, "get_retrieval_service", lambda: FakeRetrievalService())
    # Conversation store is a simple in-memory dict-backed store — no live
    # dependency to stub, but keep each test isolated from prior test state.
    prometheus.conversation_store.clear("user-1")


@pytest.mark.asyncio
async def test_rate_limit_error_yields_friendly_retry_message(monkeypatch: pytest.MonkeyPatch) -> None:
    exc = _make_status_error(429)
    monkeypatch.setattr(prometheus, "get_client", lambda: FakeClient(exc))

    chunks = await _collect(prometheus.stream_chat("user-1", "What's my balance?"))

    joined = "".join(chunks)
    assert "receiving a lot of requests" in joined
    assert "data: [DONE]" in joined
    # Must not fall through to the generic connection-error message.
    assert "trouble connecting" not in joined


@pytest.mark.asyncio
async def test_overloaded_error_yields_friendly_retry_message(monkeypatch: pytest.MonkeyPatch) -> None:
    exc = _make_status_error(529)
    monkeypatch.setattr(prometheus, "get_client", lambda: FakeClient(exc))

    chunks = await _collect(prometheus.stream_chat("user-1", "What's my balance?"))

    joined = "".join(chunks)
    assert "receiving a lot of requests" in joined
    assert "data: [DONE]" in joined
    assert "trouble connecting" not in joined


@pytest.mark.asyncio
async def test_other_api_status_error_uses_generic_message(monkeypatch: pytest.MonkeyPatch) -> None:
    exc = _make_status_error(400)
    monkeypatch.setattr(prometheus, "get_client", lambda: FakeClient(exc))

    chunks = await _collect(prometheus.stream_chat("user-1", "What's my balance?"))

    joined = "".join(chunks)
    assert "trouble connecting" in joined
    assert "receiving a lot of requests" not in joined
    assert "data: [DONE]" in joined


@pytest.mark.asyncio
async def test_non_api_exception_still_falls_back_to_generic_message(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(prometheus, "get_client", lambda: FakeClient(RuntimeError("boom")))

    chunks = await _collect(prometheus.stream_chat("user-1", "What's my balance?"))

    joined = "".join(chunks)
    assert "trouble connecting" in joined
    assert "data: [DONE]" in joined
