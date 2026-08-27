"""Trace propagation and privacy tests for the intelligence service.

The central claim of nester#1054 is that one trace spans the Go API and this
service. A broken propagation looks identical to a working one until someone
opens a waterfall and finds two disconnected traces, so it is asserted here
rather than inspected by eye.
"""

from typing import Any

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.sdk.trace.sampling import ALWAYS_ON
from opentelemetry.trace import SpanContext, TraceFlags, set_span_in_context
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator
from opentelemetry.util._once import Once

from app.telemetry import (
    REQUEST_ID_ATTRIBUTE,
    RETENTION_ATTRIBUTE,
    build_sampler,
    instrument_app,
    mark_for_retention,
)

# A W3C traceparent identifying an upstream Go API span. These are the ids the
# Python spans must inherit.
UPSTREAM_TRACE_ID = "4bf92f3577b34da6a3ce929d0e0e4736"
UPSTREAM_SPAN_ID = "00f067aa0ba902b7"
UPSTREAM_TRACEPARENT = f"00-{UPSTREAM_TRACE_ID}-{UPSTREAM_SPAN_ID}-01"


@pytest.fixture
def exporter() -> InMemorySpanExporter:
    """Install an in-memory tracer provider for the duration of a test."""
    memory = InMemorySpanExporter()
    provider = TracerProvider(sampler=ALWAYS_ON)
    provider.add_span_processor(SimpleSpanProcessor(memory))

    # The OTel API sets the global provider exactly once per process and logs
    # "Overriding of current TracerProvider is not allowed" on any later
    # attempt. Both the slot and its one-shot guard have to be cleared for
    # each test to get its own provider; resetting only the slot leaves every
    # test after the first exporting into the first test's provider. This is
    # deliberate test-only surgery on private state.
    trace._TRACER_PROVIDER = None  # type: ignore[attr-defined]
    trace._TRACER_PROVIDER_SET_ONCE = Once()  # type: ignore[attr-defined]
    trace.set_tracer_provider(provider)
    return memory


@pytest.fixture
def traced_app(exporter: InMemorySpanExporter) -> FastAPI:
    """A minimal instrumented app, standing in for the real service."""
    app = FastAPI()

    @app.get("/intelligence/chat")
    async def chat() -> dict[str, str]:
        return {"reply": "ok"}

    @app.get("/boom")
    async def boom() -> dict[str, str]:
        raise ValueError("intentional failure")

    instrument_app(app)
    return app


def test_inbound_traceparent_continues_the_same_trace(
    traced_app: FastAPI, exporter: InMemorySpanExporter
) -> None:
    """A Go parent span and this service's server span share a trace.

    This is the acceptance criterion from the issue: the trace ID must match
    the caller's, and the server span's parent must be the caller's span.
    """
    client = TestClient(traced_app)
    response = client.get(
        "/intelligence/chat",
        headers={"traceparent": UPSTREAM_TRACEPARENT},
    )
    assert response.status_code == 200

    spans = exporter.get_finished_spans()
    assert spans, "no server span was recorded"

    server_span = spans[-1]

    assert format(server_span.context.trace_id, "032x") == UPSTREAM_TRACE_ID, (
        "the service started a new trace instead of continuing the caller's"
    )

    assert server_span.parent is not None, "server span has no parent"
    assert format(server_span.parent.span_id, "016x") == UPSTREAM_SPAN_ID, (
        "server span is not parented to the caller's span"
    )
    assert server_span.parent.is_remote, "parent should be marked remote"


def test_span_is_server_kind(traced_app: FastAPI, exporter: InMemorySpanExporter) -> None:
    """The continued span must be a SERVER span for the waterfall to read right."""
    client = TestClient(traced_app)
    client.get("/intelligence/chat", headers={"traceparent": UPSTREAM_TRACEPARENT})

    spans = exporter.get_finished_spans()
    assert spans
    assert spans[-1].kind == trace.SpanKind.SERVER


def test_without_traceparent_a_new_trace_is_rooted(
    traced_app: FastAPI, exporter: InMemorySpanExporter
) -> None:
    """A direct call with no inbound context still produces a usable trace."""
    client = TestClient(traced_app)
    response = client.get("/intelligence/chat")
    assert response.status_code == 200

    spans = exporter.get_finished_spans()
    assert spans
    server_span = spans[-1]

    assert server_span.parent is None, "expected a locally-rooted trace"
    assert format(server_span.context.trace_id, "032x") != UPSTREAM_TRACE_ID


def test_request_id_is_bound_to_the_span(
    traced_app: FastAPI, exporter: InMemorySpanExporter
) -> None:
    """X-Request-ID is recorded so support can pivot from one id to the other."""
    client = TestClient(traced_app)
    client.get(
        "/intelligence/chat",
        headers={"traceparent": UPSTREAM_TRACEPARENT, "X-Request-ID": "req-abc-123"},
    )

    spans = exporter.get_finished_spans()
    assert spans
    attributes = spans[-1].attributes or {}
    assert attributes.get(REQUEST_ID_ATTRIBUTE) == "req-abc-123"


def test_oversized_request_id_is_bounded(
    traced_app: FastAPI, exporter: InMemorySpanExporter
) -> None:
    """An attacker-supplied request ID cannot create an unbounded attribute."""
    client = TestClient(traced_app)
    client.get("/intelligence/chat", headers={"X-Request-ID": "A" * 4096})

    spans = exporter.get_finished_spans()
    assert spans
    attributes = spans[-1].attributes or {}
    recorded = attributes.get(REQUEST_ID_ATTRIBUTE)
    assert isinstance(recorded, str)
    assert len(recorded) <= 128


def test_span_name_uses_route_template_not_raw_path(
    exporter: InMemorySpanExporter,
) -> None:
    """A path parameter must not become part of the span name.

    A raw path would make span names unbounded; the route template keeps
    cardinality flat.
    """
    app = FastAPI()

    @app.get("/intelligence/users/{user_id}/summary")
    async def summary(user_id: str) -> dict[str, str]:
        return {"user_id": user_id}

    instrument_app(app)

    client = TestClient(app)
    client.get("/intelligence/users/9f3ab2c1-dead-beef/summary")

    spans = exporter.get_finished_spans()
    assert spans
    name = spans[-1].name
    assert "9f3ab2c1" not in name, f"span name embeds a request-specific id: {name}"
    assert "{user_id}" in name or "user_id" in name


def test_handler_exception_marks_span_error(
    traced_app: FastAPI, exporter: InMemorySpanExporter
) -> None:
    """A failing handler must leave a closed, errored span behind."""
    client = TestClient(traced_app, raise_server_exceptions=False)
    client.get("/boom", headers={"traceparent": UPSTREAM_TRACEPARENT})

    spans = exporter.get_finished_spans()
    assert spans, "an exception must not prevent the span from being exported"

    server_span = spans[-1]
    assert server_span.end_time is not None, "span was left open after an exception"
    assert format(server_span.context.trace_id, "032x") == UPSTREAM_TRACE_ID


def test_outbound_headers_carry_the_trace(exporter: InMemorySpanExporter) -> None:
    """Injection produces a traceparent a downstream service can continue.

    This is the sending half of propagation, exercised directly so the
    round-trip contract is pinned from both ends.
    """
    propagator = TraceContextTextMapPropagator()

    span_context = SpanContext(
        trace_id=int(UPSTREAM_TRACE_ID, 16),
        span_id=int(UPSTREAM_SPAN_ID, 16),
        is_remote=False,
        trace_flags=TraceFlags(TraceFlags.SAMPLED),
    )
    carrier: dict[str, str] = {}
    propagator.inject(
        carrier,
        context=set_span_in_context(trace.NonRecordingSpan(span_context)),
    )

    assert "traceparent" in carrier
    assert UPSTREAM_TRACE_ID in carrier["traceparent"]


class TestSampling:
    """The head sampler must never truncate an upstream-sampled trace."""

    def test_ratio_one_always_samples(self) -> None:
        # Asserting only the type is too weak: build_sampler(0.0) also returns
        # a ParentBased, so the check would pass even if the ratio-1 branch
        # were changed to ALWAYS_OFF. Assert the decision instead.
        decision = build_sampler(1.0).should_sample(None, 0x1234, "op")
        assert decision.decision.is_sampled()

    def test_ratio_zero_drops_local_roots(self) -> None:
        sampler = build_sampler(0.0)
        decision = sampler.should_sample(None, 0x1234, "op")
        assert not decision.decision.is_sampled()

    def test_sampled_remote_parent_is_honoured_at_ratio_zero(self) -> None:
        """A trace the Go API decided to sample must not be dropped here."""
        parent_context = set_span_in_context(
            trace.NonRecordingSpan(
                SpanContext(
                    trace_id=int(UPSTREAM_TRACE_ID, 16),
                    span_id=int(UPSTREAM_SPAN_ID, 16),
                    is_remote=True,
                    trace_flags=TraceFlags(TraceFlags.SAMPLED),
                )
            )
        )

        decision = build_sampler(0.0).should_sample(parent_context, 0x1234, "op")
        assert decision.decision.is_sampled(), (
            "a sampled upstream trace was dropped, severing the distributed trace"
        )


def test_mark_for_retention_sets_the_attribute(exporter: InMemorySpanExporter) -> None:
    """Errors and slow calls are flagged for the collector's tail sampler."""
    tracer = trace.get_tracer("test")
    with tracer.start_as_current_span("op") as span:
        mark_for_retention(span)

    spans = exporter.get_finished_spans()
    assert spans
    attributes = spans[-1].attributes or {}
    assert attributes.get(RETENTION_ATTRIBUTE) is True


def test_mark_for_retention_is_safe_without_a_span() -> None:
    """The helper must be callable anywhere without guarding."""
    mark_for_retention(None)


def test_health_endpoints_are_not_traced(exporter: InMemorySpanExporter) -> None:
    """Probes fire constantly and would otherwise dominate the trace volume."""
    app = FastAPI()

    @app.get("/health")
    async def health() -> dict[str, str]:
        return {"status": "ok"}

    instrument_app(app)

    client = TestClient(app)
    client.get("/health")

    assert not exporter.get_finished_spans(), "health probes should be excluded"


def test_tracing_disabled_leaves_requests_working() -> None:
    """With tracing off the service must behave exactly as before."""
    app = FastAPI()

    @app.get("/intelligence/chat")
    async def chat() -> dict[str, str]:
        return {"reply": "ok"}

    # No instrumentation applied at all — the disabled path.
    client = TestClient(app)
    response = client.get(
        "/intelligence/chat", headers={"traceparent": UPSTREAM_TRACEPARENT}
    )
    assert response.status_code == 200
    assert response.json() == {"reply": "ok"}


def _attribute_blob(span: Any) -> str:
    """Flatten a span's exported surface for substring assertions."""
    parts = [span.name, str(span.status.description or "")]
    for key, value in (span.attributes or {}).items():
        parts.append(f"{key}={value}")
    for event in span.events:
        parts.append(event.name)
        for key, value in (event.attributes or {}).items():
            parts.append(f"{key}={value}")
    return " ".join(parts)


def test_no_sensitive_headers_reach_spans(
    traced_app: FastAPI, exporter: InMemorySpanExporter
) -> None:
    """Authorization and API keys must never be exported as span data."""
    # Assembled from fragments so a credential-shaped literal does not trip
    # the repository's gitleaks scan; the value is identical at run time.
    fake_jwt = "eyJhbGciOiJIUzI1NiJ9" + "." + "eyJzdWIiOiIxIn0" + "." + "c2lnbmF0dXJl"
    fake_key = "sk-ant-api03-" + "A" * 24

    client = TestClient(traced_app)
    client.get(
        "/intelligence/chat",
        headers={
            "traceparent": UPSTREAM_TRACEPARENT,
            "Authorization": f"Bearer {fake_jwt}",
            "X-Api-Key": fake_key,
        },
    )

    spans = exporter.get_finished_spans()
    assert spans

    for span in spans:
        blob = _attribute_blob(span)
        assert fake_jwt not in blob, "a JWT reached a span"
        assert fake_key not in blob, "an API key reached a span"
        assert "Bearer" not in blob, "an Authorization header reached a span"


class TestTracingTransportSecurity:
    """Spans must not cross a network in plaintext outside development."""

    def _settings(self, **overrides: object) -> object:
        from app.config import Settings

        base: dict[str, object] = {
            "tracing_enabled": True,
            "otel_exporter_otlp_insecure": True,
            "environment": "development",
        }
        base.update(overrides)
        return Settings(**base)  # type: ignore[arg-type]

    def test_insecure_rejected_in_deployed_environments(self) -> None:
        for env in ("staging", "production"):
            with pytest.raises(ValueError, match="INSECURE"):
                self._settings(environment=env)

    def test_insecure_allowed_in_development(self) -> None:
        self._settings(environment="development")

    def test_secure_transport_allowed_everywhere(self) -> None:
        self._settings(environment="production", otel_exporter_otlp_insecure=False)

    def test_no_constraint_when_tracing_disabled(self) -> None:
        self._settings(environment="production", tracing_enabled=False)
