package config

import (
	"strings"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/breaker"
)

func TestCircuitBreakerDefaults(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if !cfg.CircuitBreaker().Enabled() {
		t.Error("expected the chain circuit breakers to be enabled by default")
	}

	policy := cfg.CircuitBreaker().Policy()
	if policy.FailureRatio != breaker.DefaultFailureRatio {
		t.Errorf("failure ratio = %v, want %v", policy.FailureRatio, breaker.DefaultFailureRatio)
	}
	if policy.MinRequests != breaker.DefaultMinRequests {
		t.Errorf("min requests = %d, want %d", policy.MinRequests, breaker.DefaultMinRequests)
	}
	if policy.Window != breaker.DefaultWindow {
		t.Errorf("window = %v, want %v", policy.Window, breaker.DefaultWindow)
	}
	if policy.OpenDuration != breaker.DefaultOpenDuration {
		t.Errorf("open duration = %v, want %v", policy.OpenDuration, breaker.DefaultOpenDuration)
	}
}

func TestCircuitBreakerOverrides(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("CIRCUIT_BREAKER_FAILURE_RATIO", "0.75")
	t.Setenv("CIRCUIT_BREAKER_MIN_REQUESTS", "25")
	t.Setenv("CIRCUIT_BREAKER_WINDOW", "2m")
	t.Setenv("CIRCUIT_BREAKER_OPEN_DURATION", "45s")
	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	policy := cfg.CircuitBreaker().Policy()
	if policy.FailureRatio != 0.75 {
		t.Errorf("failure ratio = %v, want 0.75", policy.FailureRatio)
	}
	if policy.MinRequests != 25 {
		t.Errorf("min requests = %d, want 25", policy.MinRequests)
	}
	if policy.Window != 2*time.Minute {
		t.Errorf("window = %v, want 2m", policy.Window)
	}
	if policy.OpenDuration != 45*time.Second {
		t.Errorf("open duration = %v, want 45s", policy.OpenDuration)
	}
}

// The kill switch. A resilience mechanism can itself cause an outage if its
// thresholds are wrong for an environment, so turning it off must not require
// a code change.
func TestCircuitBreakerCanBeDisabled(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("CIRCUIT_BREAKER_ENABLED", "false")
	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.CircuitBreaker().Enabled() {
		t.Error("expected the circuit breakers to be disabled")
	}
}

// A policy that cannot behave sensibly is refused at startup rather than
// discovered when it sheds all traffic or none.
func TestCircuitBreakerRejectsUnusablePolicies(t *testing.T) {
	cases := map[string]map[string]string{
		"ratio of zero":      {"CIRCUIT_BREAKER_FAILURE_RATIO": "0"},
		"negative ratio":     {"CIRCUIT_BREAKER_FAILURE_RATIO": "-0.5"},
		"ratio above one":    {"CIRCUIT_BREAKER_FAILURE_RATIO": "1.5"},
		"zero min requests":  {"CIRCUIT_BREAKER_MIN_REQUESTS": "0"},
		"negative min":       {"CIRCUIT_BREAKER_MIN_REQUESTS": "-1"},
		"zero window":        {"CIRCUIT_BREAKER_WINDOW": "0s"},
		"zero open duration": {"CIRCUIT_BREAKER_OPEN_DURATION": "0s"},
	}

	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			baseEnv(t)
			requiredEnv(t)
			for key, value := range env {
				t.Setenv(key, value)
			}
			chdir(t, t.TempDir())

			_, err := Load()
			if err == nil {
				t.Fatalf("expected Load() to reject %v", env)
			}
			if !strings.Contains(err.Error(), "CIRCUIT_BREAKER") {
				t.Errorf("expected the error to name the setting, got %v", err)
			}
		})
	}
}

// A disabled breaker's thresholds are never read, so they must not be able to
// block startup. Otherwise the kill switch could not be used to recover from a
// bad configuration.
func TestDisabledCircuitBreakerSkipsPolicyValidation(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("CIRCUIT_BREAKER_ENABLED", "false")
	t.Setenv("CIRCUIT_BREAKER_FAILURE_RATIO", "0")
	chdir(t, t.TempDir())

	if _, err := Load(); err != nil {
		t.Fatalf("a disabled breaker's thresholds blocked startup: %v", err)
	}
}
