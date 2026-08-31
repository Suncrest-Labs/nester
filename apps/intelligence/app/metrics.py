"""Prometheus instrumentation for the intelligence service (nester#1056).

The Go API already exposes request-level metrics for calls it makes *to* this
service (``nester_outbound_*{upstream="intelligence"}``). That view is enough
for an availability SLI but cannot see two things that only exist inside this
process:

* **Time to first token.** Perceived responsiveness of a streamed answer is
  dominated by TTFT, not by total duration. The Go API sees one long HTTP
  request and cannot tell a stream that started instantly from one that
  stalled ten seconds before its first byte.

* **Refusals.** A guardrail or grounding refusal is a ``200 OK`` carrying a
  polite sentence. Every transport-level metric reports it as a success, which
  is correct — the system worked — but a refusal *wave* caused by a
  misconfigured guardrail is invisible to those metrics while being extremely
  visible to users.

``telemetry_llm.py`` already records both as span attributes. Spans are
sampled, so they cannot carry an SLO: at a 10% sample ratio the error budget
would be computed from a tenth of the events, and a rare failure could vanish
entirely. These metrics are unsampled counters and histograms recorded
alongside the existing spans, not instead of them.

Cardinality policy matches the Go side (see ``docs/observability/metrics.md``):
every label is a closed constant set defined in this module. A user ID, a
conversation ID, a request ID, a prompt, a model answer, or a raw exception
string is never a label. Prompt and answer text in particular must never reach
a label — it is user financial data, it is unbounded, and a metrics backend is
not a system of record for it.
"""

from __future__ import annotations

from typing import Final

from prometheus_client import CollectorRegistry, Counter, Histogram

# Namespace matching the Go API, so a scrape from a mixed environment is
# attributable without guessing. The service is distinguished by the
# ``service`` label Prometheus attaches at scrape time, not by the metric name.
_NAMESPACE: Final = "nester"
_SUBSYSTEM: Final = "intelligence"

# A dedicated registry rather than the global default, for the same reason the
# Go side uses one: exposition stays free of collectors registered incidentally
# by dependencies, and tests can build an isolated registry without the
# duplicate-registration errors that plague module-level metrics.
REGISTRY: Final = CollectorRegistry()

# Request outcomes. A closed set:
#
#   answered  - the model produced a substantive answer.
#   refused   - guardrails or grounding declined to answer. A *product*
#               outcome, not an availability failure: the service worked
#               exactly as designed. Counted separately so a refusal wave is
#               visible without burning the availability budget.
#   error     - the request failed: upstream error, timeout, unhandled
#               exception. Counts against the availability budget.
#   cancelled - the client disconnected before the answer completed. Excluded
#               from the availability denominator, because the service was not
#               given the chance to succeed or fail.
OUTCOME_ANSWERED: Final = "answered"
OUTCOME_REFUSED: Final = "refused"
OUTCOME_ERROR: Final = "error"
OUTCOME_CANCELLED: Final = "cancelled"

# Why a refusal happened. Closed set, mirroring the two mechanisms that
# produce one. Never the matched pattern or the user's text.
#
#   guardrail - the request was declined by the guardrail layer (off-topic,
#               injection-shaped, disallowed category).
#   grounding - the model had no account data to answer from and returned the
#               standard data-missing sentence.
REFUSAL_GUARDRAIL: Final = "guardrail"
REFUSAL_GROUNDING: Final = "grounding"

# TTFT buckets. A first token under ~1s reads as instant, 1-3s as responsive,
# and beyond ~5s users start abandoning, so resolution is concentrated below
# 5s. The 10s and 30s buckets exist to separate "slow" from "the upstream
# stalled", which are different incidents with different runbook entries.
_TTFT_BUCKETS: Final = (0.1, 0.25, 0.5, 1.0, 1.5, 2.0, 3.0, 5.0, 10.0, 30.0)

# Total-duration buckets reach further out: a long grounded answer with
# several tool rounds legitimately takes tens of seconds, and the tail is where
# the timeout budget is decided.
_DURATION_BUCKETS: Final = (0.5, 1.0, 2.5, 5.0, 10.0, 20.0, 30.0, 60.0, 120.0)

requests_total: Final = Counter(
    "requests_total",
    "Intelligence requests by terminal outcome.",
    labelnames=("outcome",),
    namespace=_NAMESPACE,
    subsystem=_SUBSYSTEM,
    registry=REGISTRY,
)

refusals_total: Final = Counter(
    "refusals_total",
    "Intelligence requests refused, by refusal reason.",
    labelnames=("reason",),
    namespace=_NAMESPACE,
    subsystem=_SUBSYSTEM,
    registry=REGISTRY,
)

time_to_first_token_seconds: Final = Histogram(
    "time_to_first_token_seconds",
    "Seconds from request accepted to first streamed token.",
    buckets=_TTFT_BUCKETS,
    namespace=_NAMESPACE,
    subsystem=_SUBSYSTEM,
    registry=REGISTRY,
)

request_duration_seconds: Final = Histogram(
    "request_duration_seconds",
    "Seconds from request accepted to the answer completing, by outcome.",
    labelnames=("outcome",),
    buckets=_DURATION_BUCKETS,
    namespace=_NAMESPACE,
    subsystem=_SUBSYSTEM,
    registry=REGISTRY,
)


def record_first_token(seconds: float) -> None:
    """Record time to first token.

    Negative or non-finite values are dropped rather than observed: a clock
    adjustment must not be able to poison a latency percentile that an SLO is
    computed from.
    """
    if seconds < 0 or seconds != seconds:  # NaN is the only value != itself.
        return

    time_to_first_token_seconds.observe(seconds)


def record_request(outcome: str, duration_seconds: float | None = None) -> None:
    """Record one terminal request outcome, and optionally its duration."""
    requests_total.labels(outcome=outcome).inc()

    if duration_seconds is None or duration_seconds < 0 or duration_seconds != duration_seconds:
        return

    request_duration_seconds.labels(outcome=outcome).observe(duration_seconds)


def record_refusal(reason: str) -> None:
    """Record one refusal and its reason.

    The caller records the request outcome separately; a refusal is both a
    terminal outcome and a refusal-reason event, and the two counters answer
    different questions ("what is the refusal rate" vs "which mechanism is
    refusing").
    """
    refusals_total.labels(reason=reason).inc()
