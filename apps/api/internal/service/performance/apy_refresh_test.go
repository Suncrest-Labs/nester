package performance

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

func TestWeightedVaultAPY(t *testing.T) {
	v := vault.Vault{
		ID: uuid.New(),
		Allocations: []vault.Allocation{
			{Protocol: "aave", Amount: decimal.RequireFromString("600")},
			{Protocol: "blend", Amount: decimal.RequireFromString("400")},
		},
	}

	bps, breakdown, err := weightedVaultAPY(v, map[string]uint32{
		"aave":  500,
		"blend": 700,
	})
	if err != nil {
		t.Fatalf("weightedVaultAPY() error = %v", err)
	}
	if bps != 580 {
		t.Fatalf("bps = %d, want 580", bps)
	}
	if len(breakdown) != 2 {
		t.Fatalf("breakdown len = %d, want 2", len(breakdown))
	}
}

func TestAbsDiffBPS(t *testing.T) {
	if got := absDiffBPS(500, 560); got != 60 {
		t.Fatalf("absDiffBPS = %d, want 60", got)
	}
}
