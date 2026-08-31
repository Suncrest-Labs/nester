package telemetry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestInitDisabledUsesNoopProvider(t *testing.T) {
	provider, shutdown, err := Init(context.Background(), Config{Enabled: false}, discardLogger())
	if err != nil {
		t.Fatalf("Init returned an error with tracing disabled: %v", err)
	}
	if provider == nil {
		t.Fatal("Init returned a nil provider")
	}
	if shutdown == nil {
		t.Fatal("Init returned a nil shutdown func; callers defer it unconditionally")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown of a disabled provider errored: %v", err)
	}

	// A disabled provider must still produce usable (non-recording) spans so
	// call sites need no guard.
	_, span := provider.Tracer("test").Start(context.Background(), "op")
	if span.IsRecording() {
		t.Error("disabled provider produced a recording span")
	}
	span.End()
}

// Propagators must be installed even when tracing is off, so an instance with
// tracing disabled does not sever a trace its neighbours are recording.
func TestInitInstallsPropagatorWhenDisabled(t *testing.T) {
	if _, _, err := Init(context.Background(), Config{Enabled: false}, discardLogger()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	fields := otel.GetTextMapPropagator().Fields()
	var hasTraceparent, hasTracestate bool
	for _, f := range fields {
		switch f {
		case "traceparent":
			hasTraceparent = true
		case "tracestate":
			hasTracestate = true
		}
	}
	if !hasTraceparent {
		t.Error("traceparent not propagated; W3C trace context is required by #1054")
	}
	if !hasTracestate {
		t.Error("tracestate not propagated; W3C trace context is required by #1054")
	}
}

// The collector being unreachable must not prevent startup or break requests.
// Port 1 is reserved and never listening, so this exercises the real failure
// path rather than a mock.
func TestInitToleratesUnreachableCollector(t *testing.T) {
	ctx := context.Background()
	provider, shutdown, err := Init(ctx, Config{
		Enabled:         true,
		Endpoint:        "127.0.0.1:1",
		Insecure:        true,
		ServiceName:     "nester-api-test",
		ExporterTimeout: 200 * time.Millisecond,
		SampleRatio:     1,
	}, discardLogger())
	if err != nil {
		t.Fatalf("Init failed against an unreachable collector: %v", err)
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = shutdown(shutCtx)
	}()

	// Recording spans against a dead collector must not panic or block.
	_, span := provider.Tracer("test").Start(ctx, "op")
	span.SetAttributes(SafeAttribute("http.route", "/api/v1/health"))
	RecordError(span, errors.New("business failure"))
	span.End()
}

func TestNewSamplerRespectsRatioBounds(t *testing.T) {
	tests := []struct {
		name        string
		ratio       float64
		wantSampled bool
	}{
		{"always sample at 1.0", 1, true},
		{"never sample at 0", 0, false},
		{"above 1 clamps to always", 1.5, true},
		{"below 0 clamps to never", -0.5, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sampler := NewSampler(tc.ratio)
			result := sampler.ShouldSample(sdktrace.SamplingParameters{
				ParentContext: context.Background(),
				Name:          "op",
			})
			sampled := result.Decision == sdktrace.RecordAndSample
			if sampled != tc.wantSampled {
				t.Fatalf("ratio %v: sampled = %v, want %v", tc.ratio, sampled, tc.wantSampled)
			}
		})
	}
}

// A sampled upstream decision must be honoured even where this service's own
// base ratio is zero, or a distributed trace would be truncated at this hop.
func TestSamplerHonoursSampledParent(t *testing.T) {
	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), parent)

	result := NewSampler(0).ShouldSample(sdktrace.SamplingParameters{
		ParentContext: ctx,
		Name:          "child",
	})

	if result.Decision != sdktrace.RecordAndSample {
		t.Fatalf("sampled remote parent was dropped at ratio 0; decision = %v", result.Decision)
	}
}

// Errors must be retained regardless of the base sample rate. RecordError
// applies the marker the collector's tail sampler keys on.
func TestRecordErrorMarksTraceForRetention(t *testing.T) {
	rec := newRecorder()
	_, span := rec.provider.Tracer("test").Start(context.Background(), "op")
	RecordError(span, errors.New("deposit failed"))
	span.End()

	spans := rec.exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	var found bool
	for _, attr := range spans[0].Attributes {
		if attr.Key == RetentionAttributeKey && attr.Value.AsBool() {
			found = true
		}
	}
	if !found {
		t.Errorf("error span missing %s; errors would be dropped by tail sampling", RetentionAttributeKey)
	}
}

// An error message that interpolates a secret must not reach the span.
func TestRecordErrorRedactsSecretsInMessage(t *testing.T) {
	rec := newRecorder()
	_, span := rec.provider.Tracer("test").Start(context.Background(), "op")
	RecordError(span, errors.New("invalid operator secret: "+fakeStellarSecret))
	span.End()

	spans := rec.exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	if got := spans[0].Status.Description; strings.Contains(got, fakeStellarSecret) {
		t.Errorf("span status leaked a secret seed: %q", got)
	}
	for _, event := range spans[0].Events {
		for _, attr := range event.Attributes {
			if strings.Contains(attr.Value.AsString(), fakeStellarSecret) {
				t.Errorf("span event %q leaked a secret seed", event.Name)
			}
		}
	}
}

// recorder bundles an in-memory exporter with a provider wired to it, so
// tests can assert on exactly what would have been exported.
type recorder struct {
	provider *sdktrace.TracerProvider
	exporter *tracetest.InMemoryExporter
}

// tracetest builds a provider that records every span to memory.
func newRecorder() *recorder {
	exporter := tracetest.NewInMemoryExporter()
	return &recorder{
		provider: sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sdktrace.AlwaysSample()),
			sdktrace.WithSyncer(exporter),
		),
		exporter: exporter,
	}
}

// A head ratio below 1.0 silently undercuts the collector's always-keep
// guarantees: the tail sampler can only retain traces it actually receives.
// It is the one sampling mistake with no visible symptom, so Init warns.
func TestInitWarnsWhenHeadSamplingUnderminesTailRetention(t *testing.T) {
	var logged strings.Builder
	logger := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn}))

	_, shutdown, err := Init(context.Background(), Config{
		Enabled:         true,
		Endpoint:        "127.0.0.1:1",
		Insecure:        true,
		ServiceName:     "nester-api-test",
		ExporterTimeout: 200 * time.Millisecond,
		SampleRatio:     0.05,
	}, logger)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = shutdown(ctx)
	}()

	if !strings.Contains(logged.String(), "head sampling is below 1.0") {
		t.Errorf("expected a head-sampling warning, got: %q", logged.String())
	}
}

// At 1.0 the head keeps everything and the tail sampler is authoritative, so
// there is nothing to warn about.
func TestInitDoesNotWarnAtFullSampling(t *testing.T) {
	var logged strings.Builder
	logger := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn}))

	_, shutdown, err := Init(context.Background(), Config{
		Enabled:         true,
		Endpoint:        "127.0.0.1:1",
		Insecure:        true,
		ServiceName:     "nester-api-test",
		ExporterTimeout: 200 * time.Millisecond,
		SampleRatio:     1.0,
	}, logger)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = shutdown(ctx)
	}()

	if strings.Contains(logged.String(), "head sampling is below 1.0") {
		t.Errorf("unexpected warning at full sampling: %q", logged.String())
	}
}
