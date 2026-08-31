"""Anthropic and tool-round span tests (nester#1054).

Two things are asserted here. First that the measurements the issue asks for
actually land on a span — model id, token counts, stop reason, streaming flag,
and time to first token. Second, and more important for a financial
application, that nothing else does: no prompt, no model output, no tool
argument, no API key.
"""

import time
from collections.abc import Iterator
from types import SimpleNamespace
from typing import Any

import pytest
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.sdk.trace.sampling import ALWAYS_ON
from opentelemetry.trace import StatusCode
from opentelemetry.util._once import Once

from app.telemetry import RETENTION_ATTRIBUTE
from app.telemetry_llm import (
    ATTR_DURATION_MS,
    ATTR_FINISH_REASON,
    ATTR_INPUT_TOKENS,
    ATTR_MODEL,
    ATTR_OUTPUT_TOKENS,
    ATTR_STREAM_COMPLETED,
    ATTR_STREAMING,
    ATTR_TOOL_NAME,
    ATTR_TOOL_ROUND,
    ATTR_TOOL_STATUS,
    ATTR_TTFT_MS,
    SLOW_MODEL_CALL_MS,
    async_model_call_span,
    model_call_span,
    record_tool_status,
    tool_round_span,
)

MODEL_ID = "claude-sonnet-5"

# Values that must never reach telemetry. Realistic in shape, fake in content.
USER_PROMPT = "How much is in my savings account 1234567890? I have 50000 USD."
MODEL_REPLY = "Your savings account holds 50,000 USD across two vaults."
TOOL_ARGUMENT = "G" + "A5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"
API_KEY = "sk-ant-api03-" + "Z" * 24


@pytest.fixture
def exporter() -> Iterator[InMemorySpanExporter]:
    memory = InMemorySpanExporter()
    provider = TracerProvider(sampler=ALWAYS_ON)
    provider.add_span_processor(SimpleSpanProcessor(memory))

    # The global provider is a one-shot; both it and its guard must be reset
    # so each test gets its own exporter. See tests/test_tracing.py.
    previous_provider = trace._TRACER_PROVIDER  # type: ignore[attr-defined]
    previous_once = trace._TRACER_PROVIDER_SET_ONCE  # type: ignore[attr-defined]

    trace._TRACER_PROVIDER = None  # type: ignore[attr-defined]
    trace._TRACER_PROVIDER_SET_ONCE = Once()  # type: ignore[attr-defined]
    trace.set_tracer_provider(provider)

    yield memory

    # Restore the globals so this module's providers do not leak into tests
    # that run after it.
    trace._TRACER_PROVIDER = previous_provider  # type: ignore[attr-defined]
    trace._TRACER_PROVIDER_SET_ONCE = previous_once  # type: ignore[attr-defined]


def fake_final_message(
    input_tokens: int = 1200, output_tokens: int = 340, stop_reason: str = "end_turn"
) -> Any:
    """Stand in for an Anthropic final message, matching the SDK's shape."""
    return SimpleNamespace(
        usage=SimpleNamespace(input_tokens=input_tokens, output_tokens=output_tokens),
        stop_reason=stop_reason,
        content=[SimpleNamespace(type="text", text=MODEL_REPLY)],
    )


def only_span(exporter: InMemorySpanExporter) -> Any:
    spans = exporter.get_finished_spans()
    assert len(spans) == 1, f"expected exactly 1 span, got {len(spans)}"
    return spans[0]


def attributes_of(span: Any) -> dict[str, Any]:
    return dict(span.attributes or {})


class TestModelCallAttributes:
    def test_records_model_and_streaming_flag(self, exporter: InMemorySpanExporter) -> None:
        with model_call_span(MODEL_ID, streaming=True, max_tokens=4096) as call:
            call.record_first_token()
            call.record_usage(fake_final_message())
            call.mark_completed()

        attrs = attributes_of(only_span(exporter))
        assert attrs[ATTR_MODEL] == MODEL_ID
        assert attrs[ATTR_STREAMING] is True

    def test_records_token_counts_and_stop_reason(
        self, exporter: InMemorySpanExporter
    ) -> None:
        with model_call_span(MODEL_ID, streaming=False) as call:
            call.record_usage(fake_final_message(input_tokens=987, output_tokens=123))
            call.mark_completed()

        attrs = attributes_of(only_span(exporter))
        assert attrs[ATTR_INPUT_TOKENS] == 987
        assert attrs[ATTR_OUTPUT_TOKENS] == 123
        assert list(attrs[ATTR_FINISH_REASON]) == ["end_turn"]

    def test_records_time_to_first_token(self, exporter: InMemorySpanExporter) -> None:
        """TTFT is the measurement that determines perceived responsiveness."""
        with model_call_span(MODEL_ID, streaming=True) as call:
            time.sleep(0.02)
            call.record_first_token()
            call.mark_completed()

        attrs = attributes_of(only_span(exporter))
        ttft = attrs[ATTR_TTFT_MS]
        assert isinstance(ttft, float)
        assert ttft >= 15.0, f"TTFT {ttft}ms should reflect the 20ms delay"
        assert ttft < attrs[ATTR_DURATION_MS] + 1.0

    def test_first_token_is_recorded_once(self, exporter: InMemorySpanExporter) -> None:
        """The caller invokes this per chunk; only the first may count."""
        with model_call_span(MODEL_ID, streaming=True) as call:
            call.record_first_token()
            time.sleep(0.02)
            call.record_first_token()
            call.mark_completed()

        ttft = attributes_of(only_span(exporter))[ATTR_TTFT_MS]
        assert ttft < 15.0, f"TTFT {ttft}ms was overwritten by a later chunk"

    def test_stream_that_never_yields_a_token_is_visible(
        self, exporter: InMemorySpanExporter
    ) -> None:
        """A silent stream must be distinguishable from an uninstrumented one."""
        with model_call_span(MODEL_ID, streaming=True):
            pass

        assert attributes_of(only_span(exporter))[ATTR_TTFT_MS] == -1.0

    def test_incomplete_stream_is_flagged(self, exporter: InMemorySpanExporter) -> None:
        with model_call_span(MODEL_ID, streaming=True) as call:
            call.record_first_token()
            # mark_completed deliberately not called: the client went away.

        assert attributes_of(only_span(exporter))[ATTR_STREAM_COMPLETED] is False

    def test_missing_usage_block_does_not_raise(
        self, exporter: InMemorySpanExporter
    ) -> None:
        """A shape change in the SDK must degrade the span, not break the call."""
        with model_call_span(MODEL_ID, streaming=False) as call:
            call.record_usage(SimpleNamespace())
            call.record_usage(None)
            call.mark_completed()

        attrs = attributes_of(only_span(exporter))
        assert ATTR_INPUT_TOKENS not in attrs


class TestModelCallLifecycle:
    def test_exception_closes_span_and_is_reraised(
        self, exporter: InMemorySpanExporter
    ) -> None:
        """Telemetry must never swallow a business error."""
        with pytest.raises(ValueError, match="model exploded"):
            with model_call_span(MODEL_ID, streaming=True) as call:
                call.record_first_token()
                raise ValueError("model exploded")

        span = only_span(exporter)
        assert span.end_time is not None, "span left open after an exception"
        assert span.status.status_code == StatusCode.ERROR
        assert attributes_of(span).get(RETENTION_ATTRIBUTE) is True

    def test_cancellation_closes_span(self, exporter: InMemorySpanExporter) -> None:
        """A disconnecting client raises CancelledError, not Exception.

        Catching only Exception would leak the span and silently lose the
        model call from the trace — the exact case an incident needs.
        """
        import asyncio

        with pytest.raises(asyncio.CancelledError):
            with model_call_span(MODEL_ID, streaming=True) as call:
                call.record_first_token()
                raise asyncio.CancelledError()

        span = only_span(exporter)
        assert span.end_time is not None, "span left open after cancellation"
        assert attributes_of(span).get(RETENTION_ATTRIBUTE) is True

    def test_slow_call_is_retained(self, exporter: InMemorySpanExporter) -> None:
        with model_call_span(MODEL_ID, streaming=True) as call:
            call.record_first_token()
            # Rewind the start so the recorder sees a slow call without the
            # test actually sleeping for ten seconds.
            call._started -= (SLOW_MODEL_CALL_MS / 1000.0) + 1.0  # type: ignore[attr-defined]
            call.mark_completed()

        assert attributes_of(only_span(exporter)).get(RETENTION_ATTRIBUTE) is True

    def test_fast_call_is_not_retained(self, exporter: InMemorySpanExporter) -> None:
        with model_call_span(MODEL_ID, streaming=True) as call:
            call.record_first_token()
            call.mark_completed()

        assert RETENTION_ATTRIBUTE not in attributes_of(only_span(exporter))

    @pytest.mark.asyncio
    async def test_async_variant_records_the_same_attributes(
        self, exporter: InMemorySpanExporter
    ) -> None:
        """The streaming call site uses the async form inside `async with`."""
        async with async_model_call_span(MODEL_ID, streaming=True, round_index=2) as call:
            call.record_first_token()
            call.record_usage(fake_final_message())
            call.mark_completed()

        attrs = attributes_of(only_span(exporter))
        assert attrs[ATTR_MODEL] == MODEL_ID
        assert attrs[ATTR_INPUT_TOKENS] == 1200
        assert ATTR_TTFT_MS in attrs


class TestToolRounds:
    def test_tool_span_records_name_and_round(self, exporter: InMemorySpanExporter) -> None:
        with tool_round_span("get_portfolio", 2) as span:
            record_tool_status("executed", span)

        span = only_span(exporter)
        attrs = attributes_of(span)
        assert span.name == "tool.get_portfolio"
        assert attrs[ATTR_TOOL_NAME] == "get_portfolio"
        assert attrs[ATTR_TOOL_ROUND] == 2
        assert attrs[ATTR_TOOL_STATUS] == "executed"

    def test_rejected_tool_is_retained(self, exporter: InMemorySpanExporter) -> None:
        with tool_round_span("transfer_funds", 1, consequential=True) as span:
            record_tool_status("rejected", span)

        span = only_span(exporter)
        assert span.status.status_code == StatusCode.ERROR
        assert attributes_of(span).get(RETENTION_ATTRIBUTE) is True

    def test_tool_exception_closes_span_and_reraises(
        self, exporter: InMemorySpanExporter
    ) -> None:
        with pytest.raises(RuntimeError):
            with tool_round_span("get_portfolio", 1):
                raise RuntimeError("tool blew up")

        span = only_span(exporter)
        assert span.end_time is not None
        assert span.status.status_code == StatusCode.ERROR

    def test_tool_span_nests_under_the_model_call(
        self, exporter: InMemorySpanExporter
    ) -> None:
        """The waterfall must read model call -> tool -> next model call."""
        with model_call_span(MODEL_ID, streaming=True, round_index=1) as call:
            call.record_first_token()
            with tool_round_span("get_portfolio", 1) as tool_span:
                record_tool_status("executed", tool_span)
            call.mark_completed()

        spans = exporter.get_finished_spans()
        assert len(spans) == 2

        tool_span_stub = next(s for s in spans if s.name == "tool.get_portfolio")
        model_span_stub = next(s for s in spans if s.name.startswith("anthropic."))

        assert tool_span_stub.parent is not None
        assert tool_span_stub.parent.span_id == model_span_stub.context.span_id
        assert tool_span_stub.context.trace_id == model_span_stub.context.trace_id


def exported_blob(span: Any) -> str:
    """Flatten everything a span would export, for leak assertions."""
    parts = [span.name, str(span.status.description or "")]
    for key, value in (span.attributes or {}).items():
        parts.append(f"{key}={value}")
    for event in span.events:
        parts.append(event.name)
        for key, value in (event.attributes or {}).items():
            parts.append(f"{key}={value}")
    return " ".join(parts)


class TestNoSensitiveDataLeaks:
    """The values below are why this instrumentation is risky at all."""

    def test_prompt_and_response_never_reach_a_span(
        self, exporter: InMemorySpanExporter
    ) -> None:
        with model_call_span(MODEL_ID, streaming=True) as call:
            call.record_first_token()
            call.record_usage(fake_final_message())
            call.mark_completed()

        blob = exported_blob(only_span(exporter))
        assert USER_PROMPT not in blob, "the user's prompt reached a span"
        assert MODEL_REPLY not in blob, "the model's reply reached a span"
        assert "50000" not in blob and "50,000" not in blob, "a balance reached a span"
        assert "1234567890" not in blob, "an account number reached a span"

    def test_tool_arguments_never_reach_a_span(
        self, exporter: InMemorySpanExporter
    ) -> None:
        with tool_round_span("transfer_funds", 1, consequential=True) as span:
            record_tool_status("executed", span)

        blob = exported_blob(only_span(exporter))
        assert TOOL_ARGUMENT not in blob, "a wallet address reached a tool span"

    def test_api_key_in_an_error_never_reaches_a_span(
        self, exporter: InMemorySpanExporter
    ) -> None:
        """An SDK error can interpolate the key; the span must not carry it."""
        with pytest.raises(RuntimeError):
            with model_call_span(MODEL_ID, streaming=True):
                raise RuntimeError(f"401 unauthorized for key {API_KEY}")

        blob = exported_blob(only_span(exporter))
        assert API_KEY not in blob, "an Anthropic API key reached a span"

    def test_error_status_uses_exception_type_not_message(
        self, exporter: InMemorySpanExporter
    ) -> None:
        """Status carries the type; a message could interpolate user data."""
        with pytest.raises(ValueError):
            with model_call_span(MODEL_ID, streaming=True):
                raise ValueError(USER_PROMPT)

        span = only_span(exporter)
        assert span.status.description == "ValueError"
        assert USER_PROMPT not in str(span.status.description)
