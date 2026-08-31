"""OpenTelemetry tracing for the intelligence service (nester#1054).

The Go API calls this service, and the whole point of the issue is that a
single trace should span both. That requires this service to *continue* the
inbound trace rather than start a new root — the W3C ``traceparent`` header
carries the parent span, and the FastAPI instrumentation extracts it.

Tracing is opt-in. With ``INTELLIGENCE_TRACING_ENABLED`` unset the module
installs nothing, dials no collector, and the service behaves exactly as it
did before. A missing collector is never fatal: the OTLP exporter connects
lazily and the batch processor drops spans once its queue fills, so requests
continue to be served.

Relationship to X-Request-ID: unchanged and independent. The existing
``RequestIDMiddleware`` and ``add_request_id`` middleware still mint and echo
the header exactly as before. This module additionally records the request ID
on the server span so an operator holding one identifier can find the other.

Privacy: this is a financial application and telemetry leaves the process, so
spans carry only non-sensitive, low-cardinality facts. Prompts, model
responses, tool payloads, wallet addresses, and API keys are never recorded.
See ``redact.py`` for the values that are actively stripped.
"""

import logging
from typing import TYPE_CHECKING

from opentelemetry import trace
from opentelemetry.propagate import set_global_textmap
from opentelemetry.propagators.composite import CompositePropagator
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk.trace.sampling import (
    ALWAYS_OFF,
    ALWAYS_ON,
    ParentBased,
    Sampler,
    TraceIdRatioBased,
)
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator

if TYPE_CHECKING:
    from fastapi import FastAPI

logger = logging.getLogger(__name__)

# Instrumentation scope for spans this service creates by hand, mirroring the
# Go side's telemetry.ScopeName so both halves of a trace are attributable.
SCOPE_NAME = "nester.intelligence"

# Attribute carrying the X-Request-ID value, matching the Go service's
# nester.request_id so a single query finds both halves of a trace.
REQUEST_ID_ATTRIBUTE = "nester.request_id"

# Marks a trace that must survive tail sampling regardless of the base rate.
# Matches the Go side's telemetry.RetentionAttributeKey.
RETENTION_ATTRIBUTE = "nester.force_keep"

# Endpoints excluded from tracing. Health and readiness probes fire constantly,
# carry no diagnostic value, and would otherwise dominate both the sampled
# population and the collector's ingest volume.
EXCLUDED_URLS = "health,healthz,readyz,metrics"


def build_sampler(ratio: float) -> Sampler:
    """Return the head sampler for the configured base ratio.

    ``ParentBased`` is what keeps a distributed trace whole: once the Go API
    has decided to sample a trace, this service honours that decision instead
    of re-rolling and producing a half-recorded trace. That matters most at a
    ratio of 0 — a locally-rooted trace is dropped, but an inbound sampled
    trace is still recorded.

    As on the Go side, the always-keep guarantees for errors and slow requests
    are delegated to the collector's tail sampler, because a head sampler
    decides at span start and neither outcome is known until span end.
    """
    if ratio >= 1:
        return ParentBased(ALWAYS_ON)
    if ratio <= 0:
        return ParentBased(ALWAYS_OFF)
    return ParentBased(TraceIdRatioBased(ratio))


def mark_for_retention(span: trace.Span | None = None) -> None:
    """Flag a span so tail sampling keeps its trace.

    Used for errors and for unusually slow model calls — the traces that are
    rare by definition and that uniform sampling reliably discards.
    """
    target = span if span is not None else trace.get_current_span()
    if target is None or not target.is_recording():
        return
    target.set_attribute(RETENTION_ATTRIBUTE, True)


def setup_tracing(app: "FastAPI") -> bool:
    """Configure tracing and instrument the FastAPI app.

    Returns True when tracing was enabled and installed, False when it is
    switched off or could not be configured. A failure here is logged and
    swallowed: telemetry must never prevent the service from starting.

    On success the provider is published on ``app.state.tracer_provider`` so
    the lifespan handler can flush it at shutdown. It is published only after
    instrumentation succeeds, so a half-configured provider is never exposed.
    """
    from app.config import settings

    # The propagator is installed even when tracing is disabled. Extraction is
    # what keeps a trace whole across this service; a tracing-disabled
    # instance should not sever a trace its neighbours are recording.
    set_global_textmap(CompositePropagator([TraceContextTextMapPropagator()]))

    if not settings.tracing_enabled:
        logger.info("Tracing disabled; no tracer provider installed")
        return False

    try:
        resource = Resource.create(
            {
                "service.name": settings.otel_service_name,
                "deployment.environment.name": settings.environment,
            }
        )

        provider = TracerProvider(
            resource=resource,
            sampler=build_sampler(settings.tracing_sample_ratio),
        )

        # Imported lazily so the gRPC exporter is only required when tracing
        # is actually switched on.
        from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import (
            OTLPSpanExporter,
        )

        exporter = OTLPSpanExporter(
            endpoint=settings.otel_exporter_otlp_endpoint,
            insecure=settings.otel_exporter_otlp_insecure,
            timeout=settings.otel_exporter_timeout,
        )
        provider.add_span_processor(BatchSpanProcessor(exporter))
        trace.set_tracer_provider(provider)

        instrument_app(app)

        # Published only now: instrumentation succeeded, so the provider is
        # fully wired and safe for the lifespan handler to shut down.
        app.state.tracer_provider = provider

        logger.info(
            "Tracing enabled (endpoint=%s service=%s ratio=%s)",
            settings.otel_exporter_otlp_endpoint,
            settings.otel_service_name,
            settings.tracing_sample_ratio,
        )
        return True
    except Exception:
        # Broad by intent: no telemetry misconfiguration should stop the
        # intelligence service from serving requests.
        logger.exception("Failed to configure tracing; continuing without it")
        return False


def instrument_app(app: "FastAPI") -> None:
    """Attach FastAPI instrumentation.

    Split out from setup_tracing so tests can instrument an app against their
    own in-memory provider without going through global configuration.

    The instrumentation extracts ``traceparent``/``tracestate`` from inbound
    requests and creates a SERVER span parented to the caller's span, which is
    the cross-service link the issue requires.
    """
    from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor

    FastAPIInstrumentor.instrument_app(
        app,
        excluded_urls=EXCLUDED_URLS,
        server_request_hook=_server_request_hook,
    )


def _server_request_hook(span: trace.Span | None, scope: dict[str, object]) -> None:
    """Bind the correlation ID to the server span.

    Runs for every inbound request. The request ID is read from the raw ASGI
    headers rather than request.state because this hook runs before the
    application middleware that populates it.
    """
    if span is None or not span.is_recording():
        return

    raw_headers = scope.get("headers")
    if not isinstance(raw_headers, list):
        return

    for header in raw_headers:
        if not isinstance(header, (tuple, list)) or len(header) != 2:
            continue
        name, value = header
        if not isinstance(name, bytes) or not isinstance(value, bytes):
            continue
        if name.lower() != b"x-request-id":
            continue
        try:
            decoded = value.decode("ascii", errors="replace")
        except (UnicodeDecodeError, AttributeError):
            return
        # Bound the length: this value is attacker-controllable and an
        # unbounded attribute is both a cardinality and an ingest problem.
        span.set_attribute(REQUEST_ID_ATTRIBUTE, decoded[:128])
        return


def shutdown_tracing(app: "FastAPI") -> None:
    """Flush and shut down the tracer provider, if one was installed.

    Spans are exported through a BatchSpanProcessor, so without this the last
    batch is dropped when the process stops — losing exactly the traces that
    describe a shutdown or a failing deploy.

    Errors are logged and swallowed: a telemetry failure must never obstruct
    shutdown.
    """
    provider = getattr(app.state, "tracer_provider", None)
    if provider is None:
        return

    try:
        provider.shutdown()
    except Exception:
        logger.exception("Failed to shut down the tracer provider")


def get_tracer() -> trace.Tracer:
    """Return the tracer used for hand-written spans in this service."""
    return trace.get_tracer(SCOPE_NAME)
