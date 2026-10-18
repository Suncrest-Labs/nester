package stellar

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluateSoakSamples_ContinuousAdvanceAndPercentiles(t *testing.T) {
	samples := make([]SoakSample, 100)
	for i := 0; i < 100; i++ {
		lag := 4.8
		if i >= 98 {
			lag = 14.2
		}
		samples[i] = SoakSample{
			CursorLedger: 54_102_400 + uint64(i),
			TipLedger:    54_102_401 + uint64(i),
			LagSeconds:   lag,
			RPCError:     i == 42,
		}
	}

	report, err := EvaluateSoakSamples(samples, 0)
	if err != nil {
		t.Fatalf("EvaluateSoakSamples: %v", err)
	}
	if !report.ContinuousAdvance {
		t.Fatal("expected ContinuousAdvance = true")
	}
	if report.LagP50Seconds != 4.8 {
		t.Fatalf("LagP50Seconds = %v, want 4.8", report.LagP50Seconds)
	}
	if report.LagP99Seconds != 14.2 {
		t.Fatalf("LagP99Seconds = %v, want 14.2", report.LagP99Seconds)
	}
	if report.ErrorCount != 1 {
		t.Fatalf("ErrorCount = %d, want 1", report.ErrorCount)
	}
}

func TestEvaluateSoakSamples_DetectsRegressionAndRetentionBreach(t *testing.T) {
	samples := []SoakSample{
		{CursorLedger: 1000, TipLedger: 1001, LagSeconds: 5.0},
		{CursorLedger: 999, TipLedger: 1000 + MainnetRetentionWindowLedgers, LagSeconds: 600_000.0},
	}

	report, err := EvaluateSoakSamples(samples, 1)
	if err != nil {
		t.Fatalf("EvaluateSoakSamples: %v", err)
	}
	if report.ContinuousAdvance {
		t.Fatal("expected ContinuousAdvance = false when regression/duplicates/retention breach occur")
	}
	if report.CursorRegressions != 1 {
		t.Fatalf("CursorRegressions = %d, want 1", report.CursorRegressions)
	}
	if report.RetentionBreaches != 1 {
		t.Fatalf("RetentionBreaches = %d, want 1", report.RetentionBreaches)
	}
}

func TestMainnetSoakReportAndRegisterExist(t *testing.T) {
	reportPath := filepath.Join("..", "..", "..", "..", "docs", "observability", "indexer-mainnet-soak-report.md")
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read soak report: %v", err)
	}
	doc := string(raw)
	for _, required := range []string{
		"24-Hour Mainnet RPC Indexer Soak Report (#1550)",
		"Indexer Lag (`p50`)",
		"Indexer Lag (`p99`)",
		"RPC Request Rate",
		"processed_events",
		"mainnet.sorobanrpc.com",
	} {
		if !strings.Contains(doc, required) {
			t.Fatalf("soak report missing %q", required)
		}
	}
}
