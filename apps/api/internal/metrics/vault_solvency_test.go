package metrics

import (
	"context"
	"testing"
)

type stubVaultChainInspector struct {
	totalAssets map[string]int64
	usdcBal     map[string]int64
	adapterBal  map[string]int64
}

func (s *stubVaultChainInspector) TotalAssets(_ context.Context, vaultContract string) (int64, error) {
	return s.totalAssets[vaultContract], nil
}

func (s *stubVaultChainInspector) USDCSACBalance(_ context.Context, _, vaultContract string) (int64, error) {
	return s.usdcBal[vaultContract], nil
}

func (s *stubVaultChainInspector) AdapterPosition(_ context.Context, adapterContract, _ string) (int64, error) {
	return s.adapterBal[adapterContract], nil
}

func TestVaultSolvencyReaderAndCollector(t *testing.T) {
	inspector := &stubVaultChainInspector{
		totalAssets: map[string]int64{
			"CVAULT1": 1_000_000_000, // 100 USDC in stroops
		},
		usdcBal: map[string]int64{
			"CVAULT1": 395_000_000, // 39.5 USDC idle in SAC
		},
		adapterBal: map[string]int64{
			"CADAPTER_BLEND":    400_000_000, // 40 USDC
			"CADAPTER_AQUARIUS": 200_000_000, // 20 USDC -> total backing = 99.5 USDC (50 bps shortfall)
		},
	}

	reader := NewVaultSolvencyReader(inspector, []VaultTarget{
		{
			VaultID:          "vault-main-usdc",
			VaultContract:    "CVAULT1",
			USDCSACContract:  "CUSDC",
			AdapterContracts: []string{"CADAPTER_BLEND", "CADAPTER_AQUARIUS"},
		},
	})

	m := New()
	if err := m.RegisterVaultSolvency(reader.Source()); err != nil {
		t.Fatalf("RegisterVaultSolvency: %v", err)
	}

	// Before first PollOnce, no series are emitted.
	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather before poll: %v", err)
	}
	for _, fam := range families {
		if fam.GetName() == "nester_vault_assets_divergence_bps" {
			t.Fatal("expected solvency series to be withheld before first poll")
		}
	}

	snaps, err := reader.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("len(snaps) = %d, want 1", len(snaps))
	}
	if snaps[0].BackingAssetsStroops != 995_000_000 {
		t.Fatalf("BackingAssetsStroops = %d, want 995_000_000", snaps[0].BackingAssetsStroops)
	}
	if snaps[0].DeltaStroops != 5_000_000 {
		t.Fatalf("DeltaStroops = %d, want 5_000_000", snaps[0].DeltaStroops)
	}
	if snaps[0].DeltaBps != 50.0 {
		t.Fatalf("DeltaBps = %v, want 50.0", snaps[0].DeltaBps)
	}

	families, err = m.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather after poll: %v", err)
	}

	foundBps := false
	for _, fam := range families {
		if fam.GetName() == "nester_vault_assets_divergence_bps" {
			foundBps = true
			if got := fam.GetMetric()[0].GetGauge().GetValue(); got != 50.0 {
				t.Fatalf("nester_vault_assets_divergence_bps = %v, want 50.0", got)
			}
		}
	}
	if !foundBps {
		t.Fatal("expected nester_vault_assets_divergence_bps metric to be exported")
	}
}
