package telemetry

import (
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// NewSampler builds the head-based sampler used by the tracer provider.
//
// Sampling strategy (nester#1054)
//
// The issue requires three things at once: a configurable base rate for normal
// traffic, and unconditional retention of both errors and slow requests.
// Those cannot all be satisfied by a head sampler alone, and it is worth being
// precise about why rather than pretending otherwise.
//
// A head sampler decides at span *start*. Whether a request errors, and how
// long it takes, are only known at span *end*. So a head sampler physically
// cannot condition on either. The standard OpenTelemetry answer is a two-tier
// split, which is what this code implements:
//
//   - Head (this file): a parent-respecting TraceIDRatioBased sampler at the
//     configured ratio. It bounds how much normal traffic is recorded at all.
//     Crucially it records RecordAndSample for the sampled fraction, and for
//     the unsampled remainder it still honours an upstream sampled decision so
//     a trace started by another service is never truncated here.
//
//   - Tail (the collector): the tail_sampling processor keeps every trace
//     containing an error status and every trace exceeding the latency
//     threshold, and probabilistically samples the rest. See
//     deploy/observability/otel-collector.yaml, where those policies are
//     configured from the same TRACING_* values used here.
//
// The always-on guarantees for errors and latency therefore live in the
// collector, because that is the only layer with a whole trace in hand when
// the decision is made. To keep that guarantee meaningful the head ratio
// should be set to 1.0 wherever a tail-sampling collector is deployed —
// otherwise the head has already discarded traces the tail would have kept.
// The default of 0.05 assumes direct-to-backend export with no tail sampler.
//
// ParentBased wrapping is what preserves distributed traces: once an upstream
// service has decided to sample a trace, every downstream service honours that
// decision, so a trace is never half-recorded.
func NewSampler(ratio float64) sdktrace.Sampler {
	switch {
	case ratio >= 1:
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	case ratio <= 0:
		// Still ParentBased: a local root is dropped, but a sampled parent
		// from an upstream service is honoured so cross-service traces stay
		// whole even where this service samples nothing of its own.
		return sdktrace.ParentBased(sdktrace.NeverSample())
	default:
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))
	}
}

// RetentionAttributeKey marks a trace that local policy has decided must be
// kept regardless of the base sample ratio. The collector's tail_sampling
// processor matches on it with a boolean_attribute policy, which is what turns
// the mark into an actual keep decision for the whole trace.
//
// An attribute is used rather than tracestate because the SDK does not permit
// mutating a span context's trace state after the span has started, and the
// retention decision is only known at span end.
const RetentionAttributeKey = attribute.Key("nester.force_keep")

// MarkForRetention flags a span so tail sampling retains its trace.
//
// This is the mechanism by which an error or a slow request survives a low
// base sample ratio. It is a no-op on a nil span or one that is not recording,
// so callers need not guard it.
func MarkForRetention(span trace.Span) {
	if span == nil || !span.IsRecording() {
		return
	}
	span.SetAttributes(RetentionAttributeKey.Bool(true))
}
