package vault

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestVaultCapacityStatus(t *testing.T) {
	softCap := decimal.RequireFromString("10000")
	warningPct := 80.0

	tests := []struct {
		name               string
		vault              Vault
		wantHasLimit       bool
		wantUtilization    *float64
		wantWarning        bool
		wantThreshold      float64
	}{
		{
			name: "no capacity limit",
			vault: Vault{
				ID:             uuid.New(),
				CurrentBalance: decimal.RequireFromString("5000"),
				SoftCapacity:   nil,
			},
			wantHasLimit:    false,
			wantUtilization: nil,
			wantWarning:     false,
			wantThreshold:   DefaultCapacityWarningThreshold,
		},
		{
			name: "50% utilization - no warning",
			vault: Vault{
				ID:                 uuid.New(),
				CurrentBalance:     decimal.RequireFromString("5000"),
				SoftCapacity:       &softCap,
				CapacityWarningPct: &warningPct,
			},
			wantHasLimit:    true,
			wantUtilization: ptr(50.0),
			wantWarning:     false,
			wantThreshold:   80.0,
		},
		{
			name: "exactly 80% utilization - warning",
			vault: Vault{
				ID:                 uuid.New(),
				CurrentBalance:     decimal.RequireFromString("8000"),
				SoftCapacity:       &softCap,
				CapacityWarningPct: &warningPct,
			},
			wantHasLimit:    true,
			wantUtilization: ptr(80.0),
			wantWarning:     true,
			wantThreshold:   80.0,
		},
		{
			name: "90% utilization - warning",
			vault: Vault{
				ID:                 uuid.New(),
				CurrentBalance:     decimal.RequireFromString("9000"),
				SoftCapacity:       &softCap,
				CapacityWarningPct: &warningPct,
			},
			wantHasLimit:    true,
			wantUtilization: ptr(90.0),
			wantWarning:     true,
			wantThreshold:   80.0,
		},
		{
			name: "100% utilization - warning",
			vault: Vault{
				ID:                 uuid.New(),
				CurrentBalance:     decimal.RequireFromString("10000"),
				SoftCapacity:       &softCap,
				CapacityWarningPct: &warningPct,
			},
			wantHasLimit:    true,
			wantUtilization: ptr(100.0),
			wantWarning:     true,
			wantThreshold:   80.0,
		},
		{
			name: "uses default warning threshold when not set",
			vault: Vault{
				ID:                 uuid.New(),
				CurrentBalance:     decimal.RequireFromString("8500"),
				SoftCapacity:       &softCap,
				CapacityWarningPct: nil,
			},
			wantHasLimit:    true,
			wantUtilization: ptr(85.0),
			wantWarning:     true,
			wantThreshold:   DefaultCapacityWarningThreshold,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := tt.vault.GetCapacityStatus()

			if status.HasLimit != tt.wantHasLimit {
				t.Errorf("HasLimit = %v, want %v", status.HasLimit, tt.wantHasLimit)
			}

			if tt.wantUtilization != nil {
				if status.UtilizationPct == nil {
					t.Fatal("expected utilization percentage, got nil")
				}
				if !floatNear(*status.UtilizationPct, *tt.wantUtilization, 0.01) {
					t.Errorf("UtilizationPct = %v, want %v", *status.UtilizationPct, *tt.wantUtilization)
				}
			} else if status.UtilizationPct != nil {
				t.Errorf("expected nil utilization, got %v", *status.UtilizationPct)
			}

			if status.Warning != tt.wantWarning {
				t.Errorf("Warning = %v, want %v", status.Warning, tt.wantWarning)
			}

			if status.WarningThreshold != tt.wantThreshold {
				t.Errorf("WarningThreshold = %v, want %v", status.WarningThreshold, tt.wantThreshold)
			}
		})
	}
}

func TestCanAcceptDeposit(t *testing.T) {
	softCap := decimal.RequireFromString("10000")

	tests := []struct {
		name          string
		vault         Vault
		depositAmount string
		wantErr       error
	}{
		{
			name: "no capacity limit - always allowed",
			vault: Vault{
				ID:             uuid.New(),
				CurrentBalance: decimal.RequireFromString("5000"),
				SoftCapacity:   nil,
			},
			depositAmount: "10000",
			wantErr:       nil,
		},
		{
			name: "deposit within capacity",
			vault: Vault{
				ID:             uuid.New(),
				CurrentBalance: decimal.RequireFromString("5000"),
				SoftCapacity:   &softCap,
			},
			depositAmount: "4000",
			wantErr:       nil,
		},
		{
			name: "deposit exactly reaches capacity",
			vault: Vault{
				ID:             uuid.New(),
				CurrentBalance: decimal.RequireFromString("5000"),
				SoftCapacity:   &softCap,
			},
			depositAmount: "5000",
			wantErr:       nil,
		},
		{
			name: "deposit exceeds capacity by 1",
			vault: Vault{
				ID:             uuid.New(),
				CurrentBalance: decimal.RequireFromString("5000"),
				SoftCapacity:   &softCap,
			},
			depositAmount: "5000.000001",
			wantErr:       ErrCapacityExceeded,
		},
		{
			name: "deposit exceeds capacity significantly",
			vault: Vault{
				ID:             uuid.New(),
				CurrentBalance: decimal.RequireFromString("9000"),
				SoftCapacity:   &softCap,
			},
			depositAmount: "2000",
			wantErr:       ErrCapacityExceeded,
		},
		{
			name: "vault at capacity - no additional deposit allowed",
			vault: Vault{
				ID:             uuid.New(),
				CurrentBalance: decimal.RequireFromString("10000"),
				SoftCapacity:   &softCap,
			},
			depositAmount: "0.000001",
			wantErr:       ErrCapacityExceeded,
		},
		{
			name: "zero capacity vault - any deposit exceeds",
			vault: Vault{
				ID:             uuid.New(),
				CurrentBalance: decimal.Zero,
				SoftCapacity:   ptr(decimal.Zero),
			},
			depositAmount: "0.000001",
			wantErr:       ErrCapacityExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amount := decimal.RequireFromString(tt.depositAmount)
			err := tt.vault.CanAcceptDeposit(amount)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("CanAcceptDeposit() error = %v, want %v", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Errorf("CanAcceptDeposit() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestCapacityUtilizationEdgeCases(t *testing.T) {
	tests := []struct {
		name            string
		current         string
		capacity        string
		wantUtilization float64
	}{
		{
			name:            "zero capacity returns 0%",
			current:         "5000",
			capacity:        "0",
			wantUtilization: 0.0,
		},
		{
			name:            "zero current returns 0%",
			current:         "0",
			capacity:        "10000",
			wantUtilization: 0.0,
		},
		{
			name:            "both zero returns 0%",
			current:         "0",
			capacity:        "0",
			wantUtilization: 0.0,
		},
		{
			name:            "over 100% utilization",
			current:         "15000",
			capacity:        "10000",
			wantUtilization: 150.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := decimal.RequireFromString(tt.current)
			capacity := decimal.RequireFromString(tt.capacity)

			utilization := calculateUtilizationPct(current, capacity)

			if !floatNear(utilization, tt.wantUtilization, 0.01) {
				t.Errorf("utilization = %v, want %v", utilization, tt.wantUtilization)
			}
		})
	}
}

// Helper functions

func ptr[T any](v T) *T {
	return &v
}

func floatNear(a, b, tolerance float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}
