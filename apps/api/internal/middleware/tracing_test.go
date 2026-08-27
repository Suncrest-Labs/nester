package middleware_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/telemetry"
)

// newSpanRecorder installs a provider that records spans in memory and
// restores the previous global provider when the test ends.
func newSpanRecorder(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
	)
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	// The propagator is restored too: tests that install one would otherwise
	// leak it into every test that runs after them.
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})
	return exporter
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// The span name must be the ServeMux route pattern, not the raw URL, or a
// path containing an ID would create an unbounded number of span names.
func TestTracingUsesRoutePatternAsSpanName(t *testing.T) {
	exporter := newSpanRecorder(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/users/{id}/savings-goals", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Logging(quietLogger())(
		middleware.Tracing("nester-api", time.Second)(mux),
	)

	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/users/9f3ab2c1-0000-4000-8000-000000000000/savings-goals")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 server span, got %d", len(spans))
	}

	const wantPattern = "GET /api/v1/users/{id}/savings-goals"
	if spans[0].Name != wantPattern {
		t.Errorf("span name = %q, want the route pattern %q", spans[0].Name, wantPattern)
	}

	// The concrete ID must not appear anywhere in the span name.
	if strings.Contains(spans[0].Name, "9f3ab2c1") {
		t.Errorf("span name embeds a request-specific ID: %q", spans[0].Name)
	}

	var sawRoute bool
	for _, attr := range spans[0].Attributes {
		if attr.Key == "http.route" && attr.Value.AsString() == wantPattern {
			sawRoute = true
		}
	}
	if !sawRoute {
		t.Error("span missing http.route attribute")
	}
}

// An unmatched path must not become a span name, or a 404 scan could inflate
// span-name cardinality without limit.
func TestTracingDoesNotNameSpansAfterUnmatchedPaths(t *testing.T) {
	exporter := newSpanRecorder(t)

	mux := http.NewServeMux()
	handler := middleware.Tracing("nester-api", time.Second)(mux)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/definitely/not/a/route/8fa20b")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if strings.Contains(spans[0].Name, "8fa20b") {
		t.Errorf("unmatched path leaked into span name: %q", spans[0].Name)
	}
	if spans[0].Name != http.MethodGet {
		t.Errorf("span name = %q, want the bare method for an unmatched route", spans[0].Name)
	}
}

// X-Request-ID must survive unchanged and also be bound to the span, so an
// operator holding one identifier can reach the other.
func TestTracingPreservesRequestIDAndBindsItToSpan(t *testing.T) {
	exporter := newSpanRecorder(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {})

	handler := middleware.Logging(quietLogger())(
		middleware.Tracing("nester-api", time.Second)(mux),
	)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	const suppliedID = "req-abc-123"
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/ping", nil)
	req.Header.Set("X-Request-ID", suppliedID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	// Existing behaviour: the client-supplied ID is echoed back.
	if got := resp.Header.Get("X-Request-ID"); got != suppliedID {
		t.Errorf("X-Request-ID response header = %q, want %q", got, suppliedID)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	var bound string
	for _, attr := range spans[0].Attributes {
		if attr.Key == telemetry.RequestIDAttributeKey {
			bound = attr.Value.AsString()
		}
	}
	if bound != suppliedID {
		t.Errorf("span %s = %q, want %q", telemetry.RequestIDAttributeKey, bound, suppliedID)
	}
}

// An inbound traceparent must be continued rather than starting a new trace,
// otherwise a caller's trace is severed at this service.
func TestTracingContinuesUpstreamTrace(t *testing.T) {
	exporter := newSpanRecorder(t)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {})
	handler := middleware.Tracing("nester-api", time.Second)(mux)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	const (
		upstreamTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
		upstreamSpan  = "00f067aa0ba902b7"
	)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/ping", nil)
	req.Header.Set("traceparent", "00-"+upstreamTrace+"-"+upstreamSpan+"-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	if got := spans[0].SpanContext.TraceID().String(); got != upstreamTrace {
		t.Errorf("trace ID = %s, want the upstream trace %s", got, upstreamTrace)
	}
	if got := spans[0].Parent.SpanID().String(); got != upstreamSpan {
		t.Errorf("parent span ID = %s, want %s", got, upstreamSpan)
	}
	if !spans[0].Parent.IsRemote() {
		t.Error("parent should be marked remote")
	}
}

// A 5xx must be marked for retention so tail sampling keeps it regardless of
// the base sample rate.
func TestTracingMarksServerErrorsForRetention(t *testing.T) {
	exporter := newSpanRecorder(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /boom", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	handler := middleware.Tracing("nester-api", time.Second)(mux)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/boom")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if !hasRetentionMark(spans[0].Attributes) {
		t.Error("5xx span was not marked for retention; errors would be dropped by sampling")
	}
}

// A request slower than the threshold must also be retained.
func TestTracingMarksSlowRequestsForRetention(t *testing.T) {
	exporter := newSpanRecorder(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(30 * time.Millisecond)
	})
	handler := middleware.Tracing("nester-api", 10*time.Millisecond)(mux)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/slow")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if !hasRetentionMark(spans[0].Attributes) {
		t.Error("slow request was not marked for retention")
	}
}

// The wrapper must not hide Flusher/Hijacker from downstream handlers, or the
// /ws WebSocket endpoint would break.
func TestTracingPreservesResponseWriterCapabilities(t *testing.T) {
	newSpanRecorder(t)

	var (
		sawFlusher  bool
		sawHijacker bool
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /caps", func(w http.ResponseWriter, r *http.Request) {
		_, sawFlusher = w.(http.Flusher)
		_, sawHijacker = w.(http.Hijacker)
	})

	handler := middleware.Tracing("nester-api", time.Second)(mux)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/caps")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if !sawFlusher {
		t.Error("http.Flusher not available through the tracing wrapper")
	}
	if !sawHijacker {
		t.Error("http.Hijacker not available through the tracing wrapper; /ws upgrades would fail")
	}
}

// Tracing must be transparent when no provider is configured (the default
// no-op case) — the request must still be served normally.
func TestTracingIsTransparentWithNoopProvider(t *testing.T) {
	// Init replaces the global provider and propagator; both are restored so
	// this test cannot affect the ones that follow it.
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})

	_, _, err := telemetry.Init(context.Background(), telemetry.Config{Enabled: false}, quietLogger())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	handler := middleware.Tracing("nester-api", time.Second)(mux)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ping")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTeapot {
		t.Errorf("status = %d, want %d; tracing must not alter responses", resp.StatusCode, http.StatusTeapot)
	}
}

func hasRetentionMark(attrs []attribute.KeyValue) bool {
	for _, attr := range attrs {
		if attr.Key == telemetry.RetentionAttributeKey && attr.Value.AsBool() {
			return true
		}
	}
	return false
}
