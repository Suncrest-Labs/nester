package vault

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestDeactivateYieldSourceBlocksDeposits(t *testing.T) {
	v := Vault{
		ID: uuid.New(),
		Allocations: []Allocation{{
			ID:       uuid.New(),
			Protocol: "Aave",
			Amount:   decimal.RequireFromString("100"),
		}},
	}

	if err := v.DeactivateYieldSource("aave"); err != nil {
		t.Fatalf("DeactivateYieldSource() error = %v", err)
	}
	if err := v.CanAcceptDepositTo("AAVE", decimal.RequireFromString("1")); err != ErrYieldSourceDeactivated {
		t.Fatalf("CanAcceptDepositTo() error = %v, want %v", err, ErrYieldSourceDeactivated)
	}
}

func TestMigrationGuideAndMigration(t *testing.T) {
	v := Vault{
		ID: uuid.New(),
		Allocations: []Allocation{
			{ID: uuid.New(), Protocol: "deprecated", Amount: decimal.RequireFromString("100"), Status: AllocationStatusDeactivated},
			{ID: uuid.New(), Protocol: "aave", Amount: decimal.RequireFromString("25"), Status: AllocationStatusActive},
		},
	}

	guide, err := v.MigrationGuideFor("DEPRECATED")
	if err != nil {
		t.Fatalf("MigrationGuideFor() error = %v", err)
	}
	if len(guide.Targets) != 1 || guide.Targets[0].Protocol != "aave" {
		t.Fatalf("unexpected migration targets: %+v", guide.Targets)
	}

	if err := v.MigrateYieldSourcePosition("deprecated", "aave", decimal.RequireFromString("40")); err != nil {
		t.Fatalf("MigrateYieldSourcePosition() error = %v", err)
	}
	if got := v.Allocations[0].Amount.String(); got != "60" {
		t.Fatalf("source amount = %s, want 60", got)
	}
	if got := v.Allocations[1].Amount.String(); got != "65" {
		t.Fatalf("target amount = %s, want 65", got)
	}
	if v.Allocations[0].Status != AllocationStatusDeactivated {
		t.Fatal("source allocation should remain deactivated after migration")
	}
}

func TestMigrationRejectsInactiveTarget(t *testing.T) {
	v := Vault{Allocations: []Allocation{
		{Protocol: "old", Amount: decimal.NewFromInt(10), Status: AllocationStatusDeactivated},
		{Protocol: "new", Amount: decimal.Zero, Status: AllocationStatusDeactivated},
	}}

	if err := v.MigrateYieldSourcePosition("old", "new", decimal.NewFromInt(1)); err != ErrMigrationTargetInactive {
		t.Fatalf("error = %v, want %v", err, ErrMigrationTargetInactive)
	}
}
