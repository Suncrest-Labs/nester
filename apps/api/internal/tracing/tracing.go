// Package tracing provides opt-in OpenTelemetry tracing for the API.
//
// Tracing is disabled by default: Init is a no-op unless an OTLP exporter
// endpoint is configured, and the returned shutdown function is always safe to
// call. When disabled, Tracer returns a tracer backed by the global no-op
// provider, so instrumentation added throughout the codebase incurs negligible
// cost and the application starts normally.
package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// instrumentationName is the tracer name used for spans emitted by this
// application's own instrumentation.
const instrumentationName = "github.com/suncrestlabs/nester/apps/api"

// Config describes how the tracing pipeline should be initialised. It mirrors
// the subset of the application configuration relevant to tracing so this
// package does not depend on the config package.
type Config struct {
	// Endpoint is the OTLP gRPC exporter endpoint (host:port). Tracing is only
	// enabled when this is non-empty.
	Endpoint string
	// ServiceName is reported as the service.name resource attribute.
	ServiceName string
	// Insecure disables transport security for the OTLP exporter connection.
	Insecure bool
	// SampleRatio is the parent-based trace sampling ratio in the range [0, 1].
	SampleRatio float64
	// Environment is reported as the deployment.environment resource attribute.
	Environment string
}

// ShutdownFunc flushes and releases tracing resources. It is always non-nil and
// safe to call, even when tracing is disabled.
type ShutdownFunc func(context.Context) error

// noopShutdown is returned whenever no exporter pipeline was started.
func noopShutdown(context.Context) error { return nil }

// Init configures the global OpenTelemetry tracer provider and propagator.
//
// When cfg.Endpoint is empty tracing is disabled: no exporter is created, the
// global provider is left as the default no-op, and a no-op shutdown function
// is returned with a nil error. This is the normal path when tracing is not
// configured and must never prevent the application from starting.
//
// When cfg.Endpoint is set an OTLP/gRPC exporter is created and installed as
// the global tracer provider. The returned shutdown function flushes pending
// spans and closes the exporter; callers should invoke it during graceful
// shutdown.
func Init(ctx context.Context, cfg Config) (ShutdownFunc, error) {
	// Always install the W3C trace-context and baggage propagators so inbound
	// and outbound context propagation works regardless of whether an exporter
	// is configured.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if cfg.Endpoint == "" {
		return noopShutdown, nil
	}

	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return noopShutdown, fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			attribute.String("deployment.environment", cfg.Environment),
		),
	)
	if err != nil {
		// Fall back to a resource carrying only the service name rather than
		// failing startup over a resource-detection error.
		res = resource.NewSchemaless(semconv.ServiceName(cfg.ServiceName))
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
	)

	otel.SetTracerProvider(provider)

	return provider.Shutdown, nil
}

// Tracer returns the application tracer from the globally-configured provider.
// When tracing is disabled this is a no-op tracer, so callers can start spans
// unconditionally.
func Tracer() trace.Tracer {
	return otel.Tracer(instrumentationName)
}
