package config

import (
	"strings"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/freshness"
)

// The budget must default to the documented value rather than to whatever a
// deployment happens to set, because it is the number the API's stale flag,
// the staleness alert, and the SLO documentation are all stated against.
func TestIndexerStalenessBudgetDefaults(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if got := cfg.Indexer().StalenessBudget(); got != freshness.DefaultBudget {
		t.Errorf("staleness budget = %v, want %v", got, freshness.DefaultBudget)
	}
	if freshness.DefaultBudget != 5*time.Minute {
		t.Errorf("the documented budget is 5m; DefaultBudget is %v", freshness.DefaultBudget)
	}
}

func TestIndexerStalenessBudgetOverride(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("INDEXER_STALENESS_BUDGET", "90s")
	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if got := cfg.Indexer().StalenessBudget(); got != 90*time.Second {
		t.Errorf("staleness budget = %v, want 90s", got)
	}
}

// A non-positive budget would mark every response stale and hold the alert
// permanently firing. Refusing it at startup is far cheaper than discovering
// it when the pager will not stop.
func TestIndexerStalenessBudgetMustBePositive(t *testing.T) {
	for _, value := range []string{"0s", "-1m"} {
		t.Run(value, func(t *testing.T) {
			baseEnv(t)
			requiredEnv(t)
			t.Setenv("INDEXER_STALENESS_BUDGET", value)
			chdir(t, t.TempDir())

			_, err := Load()
			if err == nil {
				t.Fatalf("expected Load() to reject INDEXER_STALENESS_BUDGET=%s", value)
			}
			if !strings.Contains(err.Error(), "INDEXER_STALENESS_BUDGET") {
				t.Errorf("expected the error to name INDEXER_STALENESS_BUDGET, got %v", err)
			}
		})
	}
}
