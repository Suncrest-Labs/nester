package tvl

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestSnapshotCreation(t *testing.T) {
	vaultID := uuid.New()
	tvl := decimal.RequireFromString("1000.5")
	snap := Snapshot{
		ID:              uuid.New(),
		VaultID:         vaultID,
		TVLUSDC:         tvl,
		TotalDepositors: 5,
		SnapshotAt:      time.Now(),
	}

	if snap.VaultID != vaultID {
		t.Errorf("vault ID mismatch")
	}
	if !snap.TVLUSDC.Equal(tvl) {
		t.Errorf("TVL mismatch: got %s, want %s", snap.TVLUSDC, tvl)
	}
	if snap.TotalDepositors != 5 {
		t.Errorf("depositors = %d, want 5", snap.TotalDepositors)
	}
}

func TestAggregateTVLAcrossProtocols(t *testing.T) {
	tests := []struct {
		name            string
		vaultSnapshots  []Snapshot
		wantTotalTVL    string
		wantDepositors  int
		wantVaultCount  int
	}{
		{
			name: "single vault",
			vaultSnapshots: []Snapshot{
				{
					ID:              uuid.New(),
					VaultID:         uuid.New(),
					TVLUSDC:         decimal.RequireFromString("1000.000000"),
					TotalDepositors: 10,
					SnapshotAt:      time.Now(),
				},
			},
			wantTotalTVL:   "1000.000000",
			wantDepositors: 10,
			wantVaultCount: 1,
		},
		{
			name: "multiple vaults aggregate correctly",
			vaultSnapshots: []Snapshot{
				{
					ID:              uuid.New(),
					VaultID:         uuid.New(),
					TVLUSDC:         decimal.RequireFromString("1000.000000"),
					TotalDepositors: 10,
					SnapshotAt:      time.Now(),
				},
				{
					ID:              uuid.New(),
					VaultID:         uuid.New(),
					TVLUSDC:         decimal.RequireFromString("2500.500000"),
					TotalDepositors: 25,
					SnapshotAt:      time.Now(),
				},
				{
					ID:              uuid.New(),
					VaultID:         uuid.New(),
					TVLUSDC:         decimal.RequireFromString("500.250000"),
					TotalDepositors: 5,
					SnapshotAt:      time.Now(),
				},
			},
			wantTotalTVL:   "4000.750000",
			wantDepositors: 40,
			wantVaultCount: 3,
		},
		{
			name:            "empty vault list",
			vaultSnapshots:  []Snapshot{},
			wantTotalTVL:    "0.000000",
			wantDepositors:  0,
			wantVaultCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total := aggregateTVL(tt.vaultSnapshots)
			depositors := aggregateDepositors(tt.vaultSnapshots)
			vaultCount := len(tt.vaultSnapshots)

			wantTVL := decimal.RequireFromString(tt.wantTotalTVL)
			if !total.Equal(wantTVL) {
				t.Errorf("total TVL = %s, want %s", total.StringFixed(6), tt.wantTotalTVL)
			}

			if depositors != tt.wantDepositors {
				t.Errorf("total depositors = %d, want %d", depositors, tt.wantDepositors)
			}

			if vaultCount != tt.wantVaultCount {
				t.Errorf("vault count = %d, want %d", vaultCount, tt.wantVaultCount)
			}
		})
	}
}

func TestZeroTVLEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		snapshots []Snapshot
		wantTVL  string
	}{
		{
			name: "vault with zero TVL",
			snapshots: []Snapshot{
				{
					ID:              uuid.New(),
					VaultID:         uuid.New(),
					TVLUSDC:         decimal.Zero,
					TotalDepositors: 0,
					SnapshotAt:      time.Now(),
				},
			},
			wantTVL: "0.000000",
		},
		{
			name: "mixed zero and positive TVL",
			snapshots: []Snapshot{
				{
					ID:              uuid.New(),
					VaultID:         uuid.New(),
					TVLUSDC:         decimal.Zero,
					TotalDepositors: 0,
					SnapshotAt:      time.Now(),
				},
				{
					ID:              uuid.New(),
					VaultID:         uuid.New(),
					TVLUSDC:         decimal.RequireFromString("1000.000000"),
					TotalDepositors: 10,
					SnapshotAt:      time.Now(),
				},
			},
			wantTVL: "1000.000000",
		},
		{
			name: "all vaults with zero TVL",
			snapshots: []Snapshot{
				{
					ID:              uuid.New(),
					VaultID:         uuid.New(),
					TVLUSDC:         decimal.Zero,
					TotalDepositors: 0,
					SnapshotAt:      time.Now(),
				},
				{
					ID:              uuid.New(),
					VaultID:         uuid.New(),
					TVLUSDC:         decimal.Zero,
					TotalDepositors: 0,
					SnapshotAt:      time.Now(),
				},
			},
			wantTVL: "0.000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total := aggregateTVL(tt.snapshots)
			want := decimal.RequireFromString(tt.wantTVL)
			if !total.Equal(want) {
				t.Errorf("total TVL = %s, want %s", total.StringFixed(6), tt.wantTVL)
			}
		})
	}
}

func TestNegativeTVLEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		snapshots []Snapshot
		wantTVL   string
	}{
		{
			name: "single negative TVL",
			snapshots: []Snapshot{
				{
					ID:              uuid.New(),
					VaultID:         uuid.New(),
					TVLUSDC:         decimal.RequireFromString("-100.000000"),
					TotalDepositors: 0,
					SnapshotAt:      time.Now(),
				},
			},
			wantTVL: "-100.000000",
		},
		{
			name: "negative and positive TVL net positive",
			snapshots: []Snapshot{
				{
					ID:              uuid.New(),
					VaultID:         uuid.New(),
					TVLUSDC:         decimal.RequireFromString("-100.000000"),
					TotalDepositors: 0,
					SnapshotAt:      time.Now(),
				},
				{
					ID:              uuid.New(),
					VaultID:         uuid.New(),
					TVLUSDC:         decimal.RequireFromString("1000.000000"),
					TotalDepositors: 10,
					SnapshotAt:      time.Now(),
				},
			},
			wantTVL: "900.000000",
		},
		{
			name: "negative and positive TVL net negative",
			snapshots: []Snapshot{
				{
					ID:              uuid.New(),
					VaultID:         uuid.New(),
					TVLUSDC:         decimal.RequireFromString("-1000.000000"),
					TotalDepositors: 0,
					SnapshotAt:      time.Now(),
				},
				{
					ID:              uuid.New(),
					VaultID:         uuid.New(),
					TVLUSDC:         decimal.RequireFromString("100.000000"),
					TotalDepositors: 5,
					SnapshotAt:      time.Now(),
				},
			},
			wantTVL: "-900.000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total := aggregateTVL(tt.snapshots)
			want := decimal.RequireFromString(tt.wantTVL)
			if !total.Equal(want) {
				t.Errorf("total TVL = %s, want %s", total.StringFixed(6), tt.wantTVL)
			}
		})
	}
}

func TestPrecisionHandling(t *testing.T) {
	tests := []struct {
		name     string
		tvl      string
		want6Dec string
		want2Dec string
	}{
		{
			name:     "full 6 decimal precision",
			tvl:      "1234.567890",
			want6Dec: "1234.567890",
			want2Dec: "1234.57",
		},
		{
			name:     "less than 6 decimals",
			tvl:      "1234.56",
			want6Dec: "1234.560000",
			want2Dec: "1234.56",
		},
		{
			name:     "integer value",
			tvl:      "1234",
			want6Dec: "1234.000000",
			want2Dec: "1234.00",
		},
		{
			name:     "small value with precision",
			tvl:      "0.123456",
			want6Dec: "0.123456",
			want2Dec: "0.12",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tvl := decimal.RequireFromString(tt.tvl)
			got6 := tvl.StringFixed(6)
			got2 := tvl.StringFixed(2)

			if got6 != tt.want6Dec {
				t.Errorf("6-decimal format = %s, want %s", got6, tt.want6Dec)
			}
			if got2 != tt.want2Dec {
				t.Errorf("2-decimal format = %s, want %s", got2, tt.want2Dec)
			}
		})
	}
}

// Helper functions for testing aggregation logic

func aggregateTVL(snapshots []Snapshot) decimal.Decimal {
	total := decimal.Zero
	for _, snap := range snapshots {
		total = total.Add(snap.TVLUSDC)
	}
	return total
}

func aggregateDepositors(snapshots []Snapshot) int {
	total := 0
	for _, snap := range snapshots {
		total += snap.TotalDepositors
	}
	return total
}
