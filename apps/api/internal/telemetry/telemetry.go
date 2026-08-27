// Package telemetry wires OpenTelemetry tracing for the API (nester#1054).
//
// Tracing is additive and opt-in. With tracing disabled — the default — Init
// installs a no-op tracer provider, dials no collector, and every
// otel.Tracer(...).Start call in the codebase becomes a cheap no-op. This
// means instrumentation can be added at call sites unconditionally without
// making a collector a startup dependency.
//
// Relationship to X-Request-ID: the two identifiers coexist and serve
// different purposes. X-Request-ID remains the support-facing correlation ID
// that a user can quote and that appears in every log line. Trace and span IDs
// describe causality and latency across services. Middleware records the
// request ID on the server span as an attribute so an operator holding one can
// find the other; neither replaces the other.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// ScopeName is the instrumentation scope reported by spans this service
// creates by hand (as opposed to spans created by library instrumentation).
const ScopeName = "github.com/suncrestlabs/nester/apps/api"

// RequestIDAttributeKey carries the X-Request-ID value onto a span so the
// existing support workflow ("quote me your request id") can reach a trace.
const RequestIDAttributeKey = attribute.Key("nester.request_id")

// Config describes how the tracer provider should be built. It mirrors
// config.TracingConfig but is declared independently so this package does not
// import the application config and can be exercised directly from tests.
type Config struct {
	Enabled          bool
	Endpoint         string
	Insecure         bool
	ServiceName      string
	ServiceVersion   string
	Environment      string
	ExporterTimeout  time.Duration
	SampleRatio      float64
	LatencyThreshold time.Duration
}

// ShutdownFunc flushes and releases tracing resources. It is always non-nil,
// so callers can defer it unconditionally.
type ShutdownFunc func(context.Context) error

// Init configures the global tracer provider and propagator.
//
// The W3C trace context and baggage propagators are installed even when
// tracing is disabled. Propagation is what lets an upstream trace survive this
// process; keeping it on means a request passing through a tracing-disabled
// instance does not sever a trace that neighbouring services are recording.
//
// A failure to reach the collector is never fatal. The OTLP exporter connects
// lazily and buffers through a batch processor, so Init returns successfully
// even with no collector listening and the application serves traffic
// normally; spans are dropped after the buffer fills. Init returns an error
// only for a configuration that cannot produce a working provider at all.
func Init(ctx context.Context, cfg Config, logger *slog.Logger) (trace.TracerProvider, ShutdownFunc, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// Propagators go in first so they apply in the disabled case too.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Never let a bad exporter or a malformed span take down the process. The
	// default OTel error handler writes to stderr; routing it through the
	// application logger keeps telemetry failures visible but non-fatal, and
	// ensures a telemetry error is never mistaken for a business error.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Warn("opentelemetry error", "error", err)
	}))

	if !cfg.Enabled {
		provider := noop.NewTracerProvider()
		otel.SetTracerProvider(provider)
		logger.Info("tracing disabled; using no-op tracer provider")
		return provider, func(context.Context) error { return nil }, nil
	}

	res, err := buildResource(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("telemetry: build resource: %w", err)
	}

	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithTimeout(cfg.ExporterTimeout),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	// NewUnstarted rather than New: New blocks trying to establish the
	// connection, which would make a missing collector delay startup. The
	// batch processor starts the exporter and retries in the background.
	exporter := otlptracegrpc.NewUnstarted(opts...)
	if err := exporter.Start(ctx); err != nil {
		// A start failure here is a local/config problem, not an unreachable
		// collector (the gRPC connection is lazy). Degrade to no-op rather
		// than refusing to boot the API over telemetry.
		logger.Error("tracing exporter failed to start; continuing without tracing", "error", err)
		provider := noop.NewTracerProvider()
		otel.SetTracerProvider(provider)
		return provider, func(context.Context) error { return nil }, nil
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(NewSampler(cfg.SampleRatio)),
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithExportTimeout(cfg.ExporterTimeout),
		),
	)
	otel.SetTracerProvider(provider)

	logger.Info("tracing enabled",
		"endpoint", cfg.Endpoint,
		"service_name", cfg.ServiceName,
		"sample_ratio", cfg.SampleRatio,
		"latency_threshold", cfg.LatencyThreshold.String(),
	)

	// The always-keep guarantees for errors and slow requests are enforced by
	// the collector's tail sampler, which can only act on traces it receives.
	// A head ratio below 1.0 drops traces before export, so those guarantees
	// silently do not hold for the dropped fraction. This is the one sampling
	// mistake with no visible symptom — the traces simply are not there — so
	// it is surfaced at startup rather than left to the documentation.
	if cfg.SampleRatio < 1 {
		logger.Warn("head sampling is below 1.0; a tail sampler cannot retain traces the head already dropped",
			"sample_ratio", cfg.SampleRatio,
			"detail", "set TRACING_SAMPLE_RATIO=1.0 when exporting to a collector that performs tail sampling",
		)
	}

	shutdown := func(ctx context.Context) error {
		// Flush before shutdown so spans buffered at exit still reach the
		// collector; both errors are reported but neither is fatal to the
		// caller's shutdown path.
		return errors.Join(provider.ForceFlush(ctx), provider.Shutdown(ctx))
	}

	return provider, shutdown, nil
}

// buildResource assembles the resource attributes attached to every span.
// Only low-cardinality, non-sensitive identifiers belong here: this data is
// duplicated onto every single span the process emits.
func buildResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
	}
	if cfg.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersion(cfg.ServiceVersion))
	}
	if cfg.Environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentName(cfg.Environment))
	}

	return resource.New(ctx,
		resource.WithHost(),
		resource.WithProcessRuntimeVersion(),
		resource.WithAttributes(attrs...),
	)
}

// Tracer returns the named tracer from the global provider.
func Tracer() trace.Tracer {
	return otel.Tracer(ScopeName)
}
