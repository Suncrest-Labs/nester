package config

import (
	"strings"
	"testing"
)

func TestMetricsConfigDefaults(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if !cfg.Metrics().Enabled() {
		t.Error("expected metrics to be enabled by default")
	}

	// Loopback by default: the endpoint exposes internal route names and
	// traffic volumes, so reaching it from another host must be a deliberate
	// choice rather than the default.
	if got := cfg.Metrics().Addr(); got != "127.0.0.1:9090" {
		t.Errorf("expected the metrics listener to default to loopback, got %q", got)
	}
}

func TestMetricsConfigOverrides(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("METRICS_ENABLED", "false")
	t.Setenv("METRICS_ADDR", "0.0.0.0:9999")
	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Metrics().Enabled() {
		t.Error("expected METRICS_ENABLED=false to disable the listener")
	}
	if got := cfg.Metrics().Addr(); got != "0.0.0.0:9999" {
		t.Errorf("expected the configured address, got %q", got)
	}
}

// TestMetricsAddrMustBeValid keeps a typo from producing a service that boots
// fine but is silently unscrapeable.
func TestMetricsAddrMustBeValid(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("METRICS_ADDR", "not-a-host-port")
	chdir(t, t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load() to reject a malformed METRICS_ADDR")
	}
	if !strings.Contains(err.Error(), "METRICS_ADDR") {
		t.Errorf("expected the error to name METRICS_ADDR, got %v", err)
	}
}

// TestMetricsAddrMustNotEqualPublicAddress is the security guard: binding the
// metrics listener to the public address would put /metrics on the public
// interface, which is the exact outcome the separate listener exists to
// prevent.
func TestMetricsAddrMustNotEqualPublicAddress(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("SERVER_HOST", "0.0.0.0")
	t.Setenv("SERVER_PORT", "8080")
	t.Setenv("METRICS_ADDR", "0.0.0.0:8080")
	chdir(t, t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load() to reject a metrics address equal to the public address")
	}
	if !strings.Contains(err.Error(), "METRICS_ADDR") {
		t.Errorf("expected the error to name METRICS_ADDR, got %v", err)
	}
}

// TestMetricsAddrNotValidatedWhenDisabled: a disabled listener never binds, so
// its address is irrelevant and must not block startup.
func TestMetricsAddrNotValidatedWhenDisabled(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("METRICS_ENABLED", "false")
	t.Setenv("METRICS_ADDR", "not-a-host-port")
	chdir(t, t.TempDir())

	if _, err := Load(); err != nil {
		t.Fatalf("a disabled metrics listener should not validate its address: %v", err)
	}
}
