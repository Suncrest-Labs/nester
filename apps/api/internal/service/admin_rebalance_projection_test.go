package service

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

func TestProjectAutoRebalanceDeltas_EqualSplit(t *testing.T) {
	allocations := []vault.Allocation{
		{Protocol: "aave", Amount: decimal.RequireFromString("700")},
		{Protocol: "blend", Amount: decimal.RequireFromString("300")},
	}
	total := decimal.RequireFromString("1000")

	deltas := projectAutoRebalanceDeltas(allocations, total)
	if len(deltas) != 2 {
		t.Fatalf("len(deltas) = %d, want 2", len(deltas))
	}

	bySource := make(map[string]string, len(deltas))
	for _, d := range deltas {
		bySource[d.SourceID] = d.Delta
	}
	if bySource["aave"] != "-200.00000000" {
		t.Fatalf("aave delta = %q, want -200.00000000", bySource["aave"])
	}
	if bySource["blend"] != "200.00000000" {
		t.Fatalf("blend delta = %q, want 200.00000000", bySource["blend"])
	}
}
