"""Anthropic and tool-use span helpers (nester#1054).

Anthropic calls dominate intelligence latency and are the least predictable
hop in a Nester request, so they are the part of a trace an operator most
often needs. This module provides the spans for them.

Time to first token is the headline measurement. It determines perceived
responsiveness — a stream that starts in 400ms feels fast even if it runs for
six seconds, whereas one that stalls for three seconds before the first
character feels broken regardless of total duration. Nothing recorded it
before, so it was invisible.

Privacy. This is the highest-risk instrumentation surface in the service:
every value flowing through here is either a user's financial question, the
model's answer about their money, or a tool argument naming an account. The
rule is an allow-list of scalars — model id, token counts, stop reason,
durations, a boolean, a tool *name* — and never message content. Specifically
never recorded:

  - prompts or system prompts (they embed portfolio and balance context)
  - model responses or streamed text
  - tool arguments or tool results (account numbers, amounts, addresses)
  - the Anthropic API key or any authorization header
  - conversation history

Only the tool's registered name is recorded, which is a compile-time
identifier from the tool registry, not user data.
"""

import time
from collections.abc import AsyncIterator, Iterator
from contextlib import asynccontextmanager, contextmanager
from typing import Any

from opentelemetry import trace
from opentelemetry.trace import Span, SpanKind, StatusCode

from app.telemetry import get_tracer, mark_for_retention

# Span attribute names.
#
# OpenTelemetry's GenAI semantic conventions are still experimental and have
# renamed these keys more than once. The stable `gen_ai.*` names that do exist
# are used where they exist, so dashboards built on the convention keep
# working, and Nester-specific measurements the convention has no name for
# (time to first token, tool round index) are namespaced under `nester.` so
# they are obviously local rather than mistaken for standard keys.
ATTR_SYSTEM = "gen_ai.system"
ATTR_OPERATION = "gen_ai.operation.name"
ATTR_MODEL = "gen_ai.request.model"
ATTR_MAX_TOKENS = "gen_ai.request.max_tokens"
ATTR_INPUT_TOKENS = "gen_ai.usage.input_tokens"
ATTR_OUTPUT_TOKENS = "gen_ai.usage.output_tokens"
ATTR_FINISH_REASON = "gen_ai.response.finish_reasons"

ATTR_STREAMING = "nester.llm.streaming"
ATTR_TTFT_MS = "nester.llm.time_to_first_token_ms"
ATTR_DURATION_MS = "nester.llm.duration_ms"
ATTR_STREAM_COMPLETED = "nester.llm.stream_completed"
ATTR_TOOL_NAME = "nester.tool.name"
ATTR_TOOL_ROUND = "nester.tool.round"
ATTR_TOOL_STATUS = "nester.tool.status"
ATTR_TOOL_CONSEQUENTIAL = "nester.tool.consequential"
ATTR_ROUND_INDEX = "nester.llm.round"

# A model call slower than this is retained regardless of the base sample
# rate, on the same reasoning as the HTTP latency threshold: the slow calls
# are rare by definition and are exactly what an incident needs.
SLOW_MODEL_CALL_MS = 10_000.0


class ModelCallRecorder:
    """Records timings and usage onto a model-call span.

    Kept as an explicit object rather than inferred from the stream because
    the caller drives a generator: the first token arrives inside a loop the
    instrumentation does not own, and the span must be closed correctly even
    when that loop is abandoned midway.
    """

    def __init__(self, span: Span, streaming: bool) -> None:
        self._span = span
        self._streaming = streaming
        self._started = time.perf_counter()
        self._first_token_at: float | None = None
        self._completed = False

    @property
    def span(self) -> Span:
        return self._span

    def record_first_token(self) -> None:
        """Stamp the arrival of the first streamed token.

        Idempotent: the caller invokes it per chunk without tracking whether
        it has already fired, and only the first call counts.
        """
        if self._first_token_at is not None:
            return
        self._first_token_at = time.perf_counter()
        if self._span.is_recording():
            ttft_ms = (self._first_token_at - self._started) * 1000.0
            self._span.set_attribute(ATTR_TTFT_MS, round(ttft_ms, 3))

    def record_usage(self, message: Any) -> None:
        """Record token counts and stop reason from a final message.

        Reads defensively via getattr: this runs against the live Anthropic
        SDK, and a missing usage block must degrade to a less detailed span
        rather than raise inside a streaming response.
        """
        if not self._span.is_recording() or message is None:
            return

        usage = getattr(message, "usage", None)
        if usage is not None:
            input_tokens = getattr(usage, "input_tokens", None)
            output_tokens = getattr(usage, "output_tokens", None)
            if isinstance(input_tokens, int):
                self._span.set_attribute(ATTR_INPUT_TOKENS, input_tokens)
            if isinstance(output_tokens, int):
                self._span.set_attribute(ATTR_OUTPUT_TOKENS, output_tokens)

        stop_reason = getattr(message, "stop_reason", None)
        if isinstance(stop_reason, str) and stop_reason:
            # A list matches the semantic convention's plural key.
            self._span.set_attribute(ATTR_FINISH_REASON, [stop_reason])

    def mark_completed(self) -> None:
        """Note that the stream ran to completion rather than being cut off."""
        self._completed = True

    def finish(self) -> None:
        """Close out timing attributes. Safe to call more than once."""
        if not self._span.is_recording():
            return

        duration_ms = (time.perf_counter() - self._started) * 1000.0
        self._span.set_attribute(ATTR_DURATION_MS, round(duration_ms, 3))
        self._span.set_attribute(ATTR_STREAM_COMPLETED, self._completed)

        if self._streaming and self._first_token_at is None:
            # A streaming call that produced no token at all is a failure
            # mode worth seeing, and its absence would otherwise look
            # identical to a call that was never instrumented.
            self._span.set_attribute(ATTR_TTFT_MS, -1.0)

        if duration_ms >= SLOW_MODEL_CALL_MS:
            mark_for_retention(self._span)


def _record_exception_safely(span: Span, exc: BaseException) -> None:
    """Mark a span failed without exporting the exception's message.

    Both of the obvious ways to record an error leak. ``record_exception``
    writes ``str(exc)`` verbatim into an exception event, and passing a
    description to ``set_status`` has the SDK prepend the message to the type.
    An Anthropic error can interpolate the API key, and a validation error can
    interpolate the user's prompt, so neither message may be exported.

    Only the exception's *type* is recorded. It is a compile-time identifier,
    never user data, and is usually enough to classify a failure; the full
    message remains available in the service's own logs.
    """
    if not span.is_recording():
        return

    type_name = type(exc).__name__
    span.set_status(StatusCode.ERROR, type_name)
    span.add_event("exception", {"exception.type": type_name})
    mark_for_retention(span)


@contextmanager
def model_call_span(
    model: str,
    *,
    streaming: bool,
    max_tokens: int | None = None,
    round_index: int | None = None,
    operation: str = "chat",
) -> Iterator[ModelCallRecorder]:
    """Wrap one Anthropic invocation in a span.

    The span is closed on every exit path — success, exception, generator
    abandonment, cancellation — because the caller is an async generator that
    a disconnecting client can tear down at any yield point. A leaked span
    would never be exported and the trace would simply lose the model call.

    Exceptions are recorded and re-raised unchanged: telemetry must never
    swallow or mask a business error.
    """
    tracer = get_tracer()
    attributes: dict[str, Any] = {
        ATTR_SYSTEM: "anthropic",
        ATTR_OPERATION: operation,
        ATTR_MODEL: model,
        ATTR_STREAMING: streaming,
    }
    if max_tokens is not None:
        attributes[ATTR_MAX_TOKENS] = max_tokens
    if round_index is not None:
        attributes[ATTR_ROUND_INDEX] = round_index

    with tracer.start_as_current_span(
        f"anthropic.{operation}",
        kind=SpanKind.CLIENT,
        attributes=attributes,
        # end_on_exit is left on so the span closes even if the generator
        # driving it is garbage-collected without completing.
        end_on_exit=True,
        # Both default to True and both leak. record_exception writes
        # str(exc) verbatim into an exception event, and
        # set_status_on_exception prepends the message to the status
        # description. An Anthropic error interpolates the API key and a
        # validation error interpolates the user's prompt, so the SDK's
        # automatic recording is switched off and replaced with
        # _record_exception_safely, which exports only the exception type.
        record_exception=False,
        set_status_on_exception=False,
    ) as span:
        recorder = ModelCallRecorder(span, streaming)
        try:
            yield recorder
        except BaseException as exc:
            # BaseException rather than Exception: a cancelled request raises
            # asyncio.CancelledError, and a span left open on cancellation is
            # exactly the case that loses a trace during an incident.
            _record_exception_safely(span, exc)
            raise
        finally:
            recorder.finish()


@asynccontextmanager
async def async_model_call_span(
    model: str,
    *,
    streaming: bool,
    max_tokens: int | None = None,
    round_index: int | None = None,
    operation: str = "chat",
) -> AsyncIterator[ModelCallRecorder]:
    """Async form of model_call_span.

    The streaming call site is an `async with` over the Anthropic stream, and
    a sync context manager cannot participate in that statement. Providing an
    async variant lets the span nest around the stream in a single `async
    with`, which leaves the streaming body's indentation untouched and keeps
    the span's lifetime exactly bounded by the stream's.
    """
    with model_call_span(
        model,
        streaming=streaming,
        max_tokens=max_tokens,
        round_index=round_index,
        operation=operation,
    ) as recorder:
        yield recorder


@contextmanager
def tool_round_span(
    tool_name: str, round_index: int, *, consequential: bool = False
) -> Iterator[Span]:
    """Wrap a single tool invocation in a child span.

    Makes the shape the issue asks for legible in a waterfall:
    model call -> tool invocation -> tool result -> next model call.

    Only the tool's registered name is recorded. Arguments and results are
    never recorded: they carry account numbers, amounts and addresses.
    """
    tracer = get_tracer()
    with tracer.start_as_current_span(
        f"tool.{tool_name}",
        kind=SpanKind.INTERNAL,
        attributes={
            ATTR_TOOL_NAME: tool_name,
            ATTR_TOOL_ROUND: round_index,
            ATTR_TOOL_CONSEQUENTIAL: consequential,
        },
        end_on_exit=True,
        # See model_call_span: the SDK's automatic exception recording would
        # export a tool error's message, which can contain tool arguments.
        record_exception=False,
        set_status_on_exception=False,
    ) as span:
        try:
            yield span
        except BaseException as exc:
            _record_exception_safely(span, exc)
            raise


def record_tool_status(status: str, span: Span | None = None) -> None:
    """Record a tool invocation's outcome.

    Status is drawn from the service's own vocabulary — "executed",
    "rejected", "proposed", "not_found" — all fixed identifiers, never user
    data. A rejection or failure is retained for tail sampling since those
    are the rounds worth investigating.
    """
    target = span if span is not None else trace.get_current_span()
    if target is None or not target.is_recording():
        return

    target.set_attribute(ATTR_TOOL_STATUS, status)
    # "failed" is the vocabulary prometheus.py uses for a handled tool
    # failure; without it such a round would be left UNSET and dropped by
    # error-based tail sampling.
    if status in {"rejected", "error", "not_found", "failed"}:
        target.set_status(StatusCode.ERROR, status)
        mark_for_retention(target)
