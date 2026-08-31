package middleware

import (
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/suncrestlabs/nester/apps/api/internal/telemetry"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
)

// Tracing creates a server span for each request (nester#1054).
//
// Placement: this middleware must sit *inside* Logging, because it reads the
// request ID that Logging mints and records it on the span. That binding is
// what lets support go from a user-quoted X-Request-ID to a trace. The two
// identifiers remain independent — the request ID is unchanged, still minted
// and still returned in the response header by Logging.
//
// Span naming uses the http.ServeMux route pattern rather than the raw URL.
// A raw path embeds identifiers ("/api/v1/users/9f3a.../savings-goals") and
// would produce effectively unbounded distinct span names, which degrades any
// trace backend. The pattern ("GET /api/v1/users/{id}/savings-goals") is
// low-cardinality and is what an operator actually wants to group by.
//
// The route pattern is only known *after* the mux has matched, which happens
// downstream of this middleware. The span is therefore opened with a
// provisional name and renamed on the way out, once http.Request.Pattern has
// been populated. This is the documented approach for net/http and avoids
// the high-cardinality trap without needing the router to be Chi.
func Tracing(serviceName string, latencyThreshold time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Continue an upstream trace when the caller supplied one. This
			// is the receiving half of W3C propagation.
			//
			// The propagator is resolved per request, not at construction:
			// telemetry.Init installs the global one, and if the router is
			// assembled first this middleware would otherwise capture a no-op
			// propagator permanently and silently discard every inbound
			// traceparent. Spans would still be produced, just detached from
			// the caller's trace. The lookup is an atomic load.
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			// Provisional name: method plus the matched pattern is not yet
			// available. Renamed below once the mux has routed.
			spanName := r.Method

			ctx, span := otel.Tracer(telemetry.ScopeName).Start(ctx, spanName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.HTTPRequestMethodKey.String(r.Method),
					semconv.URLScheme(schemeOf(r)),
					semconv.ServerAddress(r.Host),
					semconv.UserAgentOriginal(telemetry.RedactValue(r.UserAgent())),
				),
			)
			defer span.End()

			// Bind the existing correlation ID to the span. Logging runs
			// outside this middleware and has already placed it on the
			// context; recording it here is purely additive.
			if requestID := logpkg.RequestIDFromContext(ctx); requestID != "" {
				span.SetAttributes(telemetry.RequestIDAttributeKey.String(requestID))
			}

			status, writer := newTracingResponseWriter(w)
			startedAt := time.Now()

			// WithContext returns a shallow copy, and ServeMux records the
			// matched route on whichever request value it is given. Holding
			// that copy is what makes Pattern readable afterwards; reading it
			// back off r would always find it empty.
			traced := r.WithContext(ctx)

			next.ServeHTTP(writer, traced)

			duration := time.Since(startedAt)

			// An unmatched request leaves Pattern empty, in which case the
			// span keeps a bare method name rather than adopting the raw path
			// — a 404 scan must not be able to inflate span-name cardinality.
			if pattern := traced.Pattern; pattern != "" {
				span.SetName(pattern)
				span.SetAttributes(semconv.HTTPRoute(pattern))
			}

			span.SetAttributes(semconv.HTTPResponseStatusCode(status.status))

			// Retention policy (see telemetry/sampler.go). Errors and slow
			// requests are the traces that matter during an incident and are
			// exactly the ones uniform sampling discards, so both are marked
			// for the collector's tail sampler to keep.
			if status.status >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, http.StatusText(status.status))
				telemetry.MarkForRetention(span)
			}
			if latencyThreshold > 0 && duration >= latencyThreshold {
				span.SetAttributes(attribute.Bool("nester.slow_request", true))
				telemetry.MarkForRetention(span)
			}
		})
	}
}

// tracingStatusRecorder captures the response status for the span. It is
// separate from the logging middleware's recorder so neither middleware
// depends on the other's internals.
type tracingStatusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *tracingStatusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Unwrap exposes the underlying ResponseWriter to http.ResponseController.
func (r *tracingStatusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// The recorder embeds http.ResponseWriter as an *interface*, so only that
// interface's three methods are promoted. Flush, Hijack and Push are not, and
// a direct type assertion for them against the bare recorder fails even
// though the underlying writer implements them. That matters concretely:
// gorilla/websocket asserts w.(http.Hijacker) during an upgrade, so a bare
// wrapper would break the /ws endpoint at runtime while unit tests passed.
//
// Each optional interface is therefore re-exposed explicitly, and the variant
// returned is matched to what the wrapped writer actually supports so a
// handler's capability check still reflects reality.

type flusherRecorder struct {
	*tracingStatusRecorder
	http.Flusher
}

type hijackerRecorder struct {
	*tracingStatusRecorder
	http.Hijacker
}

type flushHijackRecorder struct {
	*tracingStatusRecorder
	http.Flusher
	http.Hijacker
}

// newTracingResponseWriter wraps w so the response status can be observed,
// returning the recorder for the caller to read and the writer to pass
// downstream.
func newTracingResponseWriter(w http.ResponseWriter) (*tracingStatusRecorder, http.ResponseWriter) {
	recorder := &tracingStatusRecorder{ResponseWriter: w, status: http.StatusOK}

	flusher, canFlush := w.(http.Flusher)
	hijacker, canHijack := w.(http.Hijacker)

	switch {
	case canFlush && canHijack:
		return recorder, &flushHijackRecorder{tracingStatusRecorder: recorder, Flusher: flusher, Hijacker: hijacker}
	case canFlush:
		return recorder, &flusherRecorder{tracingStatusRecorder: recorder, Flusher: flusher}
	case canHijack:
		return recorder, &hijackerRecorder{tracingStatusRecorder: recorder, Hijacker: hijacker}
	default:
		return recorder, recorder
	}
}

func schemeOf(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	// Honour the standard proxy header; the API runs behind a TLS-terminating
	// proxy in every deployed environment.
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded == "https" {
		return "https"
	}
	return "http"
}
