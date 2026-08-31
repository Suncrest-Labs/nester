package config

import (
	"testing"
	"time"
)

// setRequiredEnv populates the variables Load treats as mandatory so a test
// can focus on the tracing settings alone.
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_DSN", "postgres://user:pass@localhost:5432/nester?sslmode=disable")
	t.Setenv("STELLAR_NETWORK_PASSPHRASE", "Test SDF Network ; September 2015")
	t.Setenv("STELLAR_RPC_URL", "https://soroban-testnet.stellar.org")
	t.Setenv("STELLAR_HORIZON_URL", "https://horizon-testnet.stellar.org")
	t.Setenv("AUTH_JWT_SECRET", "a-sufficiently-long-test-secret-value-1234567890")
	t.Setenv("APP_ENV", "test")
}

// Tracing must default to off so that neither existing deployments nor CI
// acquire a collector dependency by upgrading (nester#1054).
func TestTracingDefaultsToDisabled(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	tracing := cfg.Tracing()
	if tracing.Enabled() {
		t.Error("tracing is enabled by default; it must be opt-in")
	}
	if got, want := tracing.SampleRatio(), 0.05; got != want {
		t.Errorf("default SampleRatio = %v, want %v", got, want)
	}
	if got, want := tracing.LatencyThreshold(), 1*time.Second; got != want {
		t.Errorf("default LatencyThreshold = %v, want %v", got, want)
	}
	if got, want := tracing.ServiceName(), "nester-api"; got != want {
		t.Errorf("default ServiceName = %q, want %q", got, want)
	}
	if got, want := tracing.ExporterTimeout(), 10*time.Second; got != want {
		t.Errorf("default ExporterTimeout = %v, want %v", got, want)
	}
}

func TestTracingReadsEnvironment(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("TRACING_ENABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "collector.internal:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "false")
	t.Setenv("OTEL_SERVICE_NAME", "nester-api-staging")
	t.Setenv("OTEL_EXPORTER_TIMEOUT", "3s")
	t.Setenv("TRACING_SAMPLE_RATIO", "0.25")
	t.Setenv("TRACING_LATENCY_THRESHOLD", "2500ms")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	tracing := cfg.Tracing()
	if !tracing.Enabled() {
		t.Error("TRACING_ENABLED=true was not honoured")
	}
	if got, want := tracing.OTLPEndpoint(), "collector.internal:4317"; got != want {
		t.Errorf("OTLPEndpoint = %q, want %q", got, want)
	}
	if tracing.OTLPInsecure() {
		t.Error("OTEL_EXPORTER_OTLP_INSECURE=false was not honoured")
	}
	if got, want := tracing.ServiceName(), "nester-api-staging"; got != want {
		t.Errorf("ServiceName = %q, want %q", got, want)
	}
	if got, want := tracing.ExporterTimeout(), 3*time.Second; got != want {
		t.Errorf("ExporterTimeout = %v, want %v", got, want)
	}
	if got, want := tracing.SampleRatio(), 0.25; got != want {
		t.Errorf("SampleRatio = %v, want %v", got, want)
	}
	if got, want := tracing.LatencyThreshold(), 2500*time.Millisecond; got != want {
		t.Errorf("LatencyThreshold = %v, want %v", got, want)
	}
}

// The sample ratio is a probability; values outside [0,1] are a configuration
// error and must be reported rather than silently clamped, so an operator who
// fat-fingers a rate finds out at startup.
func TestTracingRejectsOutOfRangeSampleRatio(t *testing.T) {
	for _, ratio := range []string{"1.5", "-0.2"} {
		t.Run(ratio, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv("TRACING_SAMPLE_RATIO", ratio)

			if _, err := Load(); err == nil {
				t.Fatalf("Load accepted TRACING_SAMPLE_RATIO=%s", ratio)
			}
		})
	}
}

// A blank OTEL_EXPORTER_OTLP_ENDPOINT falls back to the default rather than
// producing an empty endpoint, because envLoader.lookup treats a whitespace-
// only value as unset. Tracing therefore stays usable instead of failing
// startup over a stray space in a deployment manifest.
func TestTracingBlankEndpointFallsBackToDefault(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("TRACING_ENABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "   ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.Tracing().OTLPEndpoint(), "localhost:4317"; got != want {
		t.Errorf("OTLPEndpoint = %q, want the default %q", got, want)
	}
}

// The endpoint guard itself is exercised directly, since env parsing cannot
// produce an empty endpoint. This protects against a future refactor that
// changes the default to "".
func TestTracingValidationRejectsEmptyEndpoint(t *testing.T) {
	loader := envLoader{fileValues: map[string]string{}, errors: []string{}}
	cfg := &Config{
		server:   ServerConfig{host: "0.0.0.0", port: 8080},
		database: DatabaseConfig{dsn: "postgres://localhost/db"},
		tracing: TracingConfig{
			enabled:         true,
			otlpEndpoint:    "",
			serviceName:     "nester-api",
			exporterTimeout: 10 * time.Second,
			sampleRatio:     0.05,
		},
	}

	cfg.validate(&loader)

	var found bool
	for _, msg := range loader.errors {
		if msg == "OTEL_EXPORTER_OTLP_ENDPOINT is required when TRACING_ENABLED is true" {
			found = true
		}
	}
	if !found {
		t.Errorf("validate did not reject an empty endpoint with tracing enabled; errors = %v", loader.errors)
	}
}

func TestTracingRejectsNonNumericSampleRatio(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("TRACING_SAMPLE_RATIO", "half")

	if _, err := Load(); err == nil {
		t.Fatal("Load accepted a non-numeric TRACING_SAMPLE_RATIO")
	}
}

// Spans must not cross a network in plaintext. The insecure default is for a
// local collector; carrying it into a deployed environment would ship
// telemetry over unencrypted gRPC.
func TestTracingRejectsInsecureExporterOutsideDevelopment(t *testing.T) {
	for _, env := range []string{"staging", "production"} {
		t.Run(env, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv("APP_ENV", env)
			t.Setenv("AUTH_JWT_SECRET", "a-sufficiently-long-non-default-secret-for-"+env)
			t.Setenv("TRACING_ENABLED", "true")
			t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")

			if _, err := Load(); err == nil {
				t.Fatalf("Load accepted an insecure OTLP exporter in %s", env)
			}
		})
	}
}

// Development is the case the insecure default exists for.
func TestTracingAllowsInsecureExporterInDevelopment(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("TRACING_ENABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")

	if _, err := Load(); err != nil {
		t.Fatalf("Load rejected the local development default: %v", err)
	}
}
