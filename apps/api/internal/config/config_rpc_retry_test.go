package config

import (
	"strings"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/retry"
)

func TestRPCRetryDefaults(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	policy := cfg.RPCRetry().Policy()
	if policy.MaxAttempts != retry.DefaultMaxAttempts {
		t.Errorf("max attempts = %d, want %d", policy.MaxAttempts, retry.DefaultMaxAttempts)
	}
	if policy.BaseDelay != retry.DefaultBaseDelay {
		t.Errorf("base delay = %v, want %v", policy.BaseDelay, retry.DefaultBaseDelay)
	}
	if policy.MaxDelay != retry.DefaultMaxDelay {
		t.Errorf("max delay = %v, want %v", policy.MaxDelay, retry.DefaultMaxDelay)
	}
	if policy.Budget != retry.DefaultBudget {
		t.Errorf("budget = %v, want %v", policy.Budget, retry.DefaultBudget)
	}
}

// The budget must stay below the server's write timeout, or the retry loop
// becomes the thing holding a request open longest and the client gets a
// truncated response instead of the typed 503.
func TestRPCRetryBudgetFitsInsideTheWriteTimeout(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	budget := cfg.RPCRetry().Policy().Budget
	if writeTimeout := cfg.Server().WriteTimeout(); budget >= writeTimeout {
		t.Fatalf("retry budget %v is not below the server write timeout %v", budget, writeTimeout)
	}
}

func TestRPCRetryOverrides(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("RPC_RETRY_MAX_ATTEMPTS", "5")
	t.Setenv("RPC_RETRY_BASE_DELAY", "250ms")
	t.Setenv("RPC_RETRY_MAX_DELAY", "4s")
	t.Setenv("RPC_RETRY_BUDGET", "9s")
	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	policy := cfg.RPCRetry().Policy()
	if policy.MaxAttempts != 5 {
		t.Errorf("max attempts = %d, want 5", policy.MaxAttempts)
	}
	if policy.BaseDelay != 250*time.Millisecond {
		t.Errorf("base delay = %v, want 250ms", policy.BaseDelay)
	}
	if policy.MaxDelay != 4*time.Second {
		t.Errorf("max delay = %v, want 4s", policy.MaxDelay)
	}
	if policy.Budget != 9*time.Second {
		t.Errorf("budget = %v, want 9s", policy.Budget)
	}
}

// One attempt turns retrying off without turning the helper off: the metrics
// and the typed error stay, only the second attempt goes away. It must be an
// accepted value, not rejected as nonsensical.
func TestRPCRetryCanBeDisabledWithASingleAttempt(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("RPC_RETRY_MAX_ATTEMPTS", "1")
	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() rejected a single-attempt policy: %v", err)
	}
	if got := cfg.RPCRetry().Policy().MaxAttempts; got != 1 {
		t.Fatalf("max attempts = %d, want 1", got)
	}
}

func TestRPCRetryRejectsUnusablePolicies(t *testing.T) {
	cases := map[string]map[string]string{
		"zero attempts":        {"RPC_RETRY_MAX_ATTEMPTS": "0"},
		"negative attempts":    {"RPC_RETRY_MAX_ATTEMPTS": "-2"},
		"zero base delay":      {"RPC_RETRY_BASE_DELAY": "0s"},
		"max delay below base": {"RPC_RETRY_BASE_DELAY": "5s", "RPC_RETRY_MAX_DELAY": "1s"},
		"zero budget":          {"RPC_RETRY_BUDGET": "0s"},
		"negative budget":      {"RPC_RETRY_BUDGET": "-1s"},
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
			if !strings.Contains(err.Error(), "RPC_RETRY") {
				t.Errorf("expected the error to name the setting, got %v", err)
			}
		})
	}
}
