package metrics

import (
	"context"
	"math"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// VaultChainInspector reads on-chain vault total_assets(), USDC SAC balance(vault),
// and adapter positions for a vault (#1547).
type VaultChainInspector interface {
	TotalAssets(ctx context.Context, vaultContract string) (int64, error)
	USDCSACBalance(ctx context.Context, usdcContract, vaultContract string) (int64, error)
	AdapterPosition(ctx context.Context, adapterContract, vaultContract string) (int64, error)
}

// VaultTarget describes a vault and its backing contracts to inspect on-chain.
type VaultTarget struct {
	VaultID          string
	VaultContract    string
	USDCSACContract  string
	AdapterContracts []string
}

// VaultSolvencySnapshot holds the computed on-chain accounting comparison for one vault.
type VaultSolvencySnapshot struct {
	VaultID                 string
	TotalAssetsStroops      int64
	USDCBalanceStroops      int64
	AdapterPositionsStroops int64
	BackingAssetsStroops    int64
	DeltaStroops            int64
	DeltaBps                float64
}

// ComputeVaultSolvencySnapshot calculates backing assets, signed delta, and
// absolute divergence in basis points between reported total_assets and
// (USDC SAC balance + adapter positions).
func ComputeVaultSolvencySnapshot(vaultID string, totalAssets, usdcBalance int64, adapterBalances []int64) VaultSolvencySnapshot {
	var adaptersSum int64
	for _, pos := range adapterBalances {
		adaptersSum += pos
	}
	backing := usdcBalance + adaptersSum
	delta := totalAssets - backing

	denom := math.Max(float64(totalAssets), float64(backing))
	var deltaBps float64
	if denom > 0 {
		deltaBps = (math.Abs(float64(delta)) * 10000.0) / denom
	} else if delta != 0 {
		deltaBps = 10000.0
	}

	return VaultSolvencySnapshot{
		VaultID:                 vaultID,
		TotalAssetsStroops:      totalAssets,
		USDCBalanceStroops:      usdcBalance,
		AdapterPositionsStroops: adaptersSum,
		BackingAssetsStroops:    backing,
		DeltaStroops:            delta,
		DeltaBps:                deltaBps,
	}
}

// VaultSolvencyReader polls on-chain vault total_assets(), USDC SAC balance(vault),
// and adapter positions on a schedule and caches the latest snapshot for scrape-time export.
type VaultSolvencyReader struct {
	inspector VaultChainInspector
	targets   []VaultTarget

	mu        sync.RWMutex
	snapshots []VaultSolvencySnapshot
	hasSample bool
}

// NewVaultSolvencyReader constructs a VaultSolvencyReader.
func NewVaultSolvencyReader(inspector VaultChainInspector, targets []VaultTarget) *VaultSolvencyReader {
	return &VaultSolvencyReader{
		inspector: inspector,
		targets:   targets,
	}
}

// PollOnce reads all configured vaults from the chain and updates the snapshot cache.
func (r *VaultSolvencyReader) PollOnce(ctx context.Context) ([]VaultSolvencySnapshot, error) {
	out := make([]VaultSolvencySnapshot, 0, len(r.targets))
	for _, target := range r.targets {
		totalAssets, err := r.inspector.TotalAssets(ctx, target.VaultContract)
		if err != nil {
			return nil, err
		}
		usdcBal, err := r.inspector.USDCSACBalance(ctx, target.USDCSACContract, target.VaultContract)
		if err != nil {
			return nil, err
		}
		adapterBals := make([]int64, 0, len(target.AdapterContracts))
		for _, adapter := range target.AdapterContracts {
			pos, err := r.inspector.AdapterPosition(ctx, adapter, target.VaultContract)
			if err != nil {
				return nil, err
			}
			adapterBals = append(adapterBals, pos)
		}
		out = append(out, ComputeVaultSolvencySnapshot(target.VaultID, totalAssets, usdcBal, adapterBals))
	}

	r.mu.Lock()
	r.snapshots = out
	r.hasSample = true
	r.mu.Unlock()

	return out, nil
}

// Source returns a VaultSolvencySource suitable for RegisterVaultSolvency.
func (r *VaultSolvencyReader) Source() VaultSolvencySource {
	return func() ([]VaultSolvencySnapshot, bool) {
		r.mu.RLock()
		defer r.mu.RUnlock()
		if !r.hasSample {
			return nil, false
		}
		cp := make([]VaultSolvencySnapshot, len(r.snapshots))
		copy(cp, r.snapshots)
		return cp, true
	}
}

// VaultSolvencySource reports the latest solvency comparison snapshots across vaults.
type VaultSolvencySource func() (snapshots []VaultSolvencySnapshot, emit bool)

type vaultSolvencyCollector struct {
	source           VaultSolvencySource
	totalAssets      *prometheus.Desc
	usdcBalance      *prometheus.Desc
	adapterPositions *prometheus.Desc
	backingAssets    *prometheus.Desc
	divergenceStroop *prometheus.Desc
	divergenceBps    *prometheus.Desc
}

func newVaultSolvencyCollector(source VaultSolvencySource) *vaultSolvencyCollector {
	labels := []string{"vault_id"}
	return &vaultSolvencyCollector{
		source: source,
		totalAssets: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "vault", "total_assets_stroops"),
			"On-chain total_assets() reported by the vault contract in stroops.",
			labels, nil,
		),
		usdcBalance: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "vault", "usdc_sac_balance_stroops"),
			"Actual on-chain USDC SAC balance(vault) in stroops.",
			labels, nil,
		),
		adapterPositions: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "vault", "adapter_positions_stroops"),
			"Sum of on-chain yield adapter positions held by the vault in stroops.",
			labels, nil,
		),
		backingAssets: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "vault", "backing_assets_stroops"),
			"Sum of USDC SAC balance(vault) and all adapter positions in stroops.",
			labels, nil,
		),
		divergenceStroop: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "vault", "assets_divergence_stroops"),
			"Signed difference (total_assets - backing_assets) in stroops.",
			labels, nil,
		),
		divergenceBps: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "vault", "assets_divergence_bps"),
			"Absolute divergence between vault total_assets and backing USDC + adapter positions in basis points.",
			labels, nil,
		),
	}
}

func (c *vaultSolvencyCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.totalAssets
	ch <- c.usdcBalance
	ch <- c.adapterPositions
	ch <- c.backingAssets
	ch <- c.divergenceStroop
	ch <- c.divergenceBps
}

func (c *vaultSolvencyCollector) Collect(ch chan<- prometheus.Metric) {
	if c.source == nil {
		return
	}
	snapshots, emit := c.source()
	if !emit {
		return
	}
	for _, s := range snapshots {
		ch <- prometheus.MustNewConstMetric(c.totalAssets, prometheus.GaugeValue, float64(s.TotalAssetsStroops), s.VaultID)
		ch <- prometheus.MustNewConstMetric(c.usdcBalance, prometheus.GaugeValue, float64(s.USDCBalanceStroops), s.VaultID)
		ch <- prometheus.MustNewConstMetric(c.adapterPositions, prometheus.GaugeValue, float64(s.AdapterPositionsStroops), s.VaultID)
		ch <- prometheus.MustNewConstMetric(c.backingAssets, prometheus.GaugeValue, float64(s.BackingAssetsStroops), s.VaultID)
		ch <- prometheus.MustNewConstMetric(c.divergenceStroop, prometheus.GaugeValue, float64(s.DeltaStroops), s.VaultID)
		ch <- prometheus.MustNewConstMetric(c.divergenceBps, prometheus.GaugeValue, s.DeltaBps, s.VaultID)
	}
}

// RegisterVaultSolvency attaches the vault total_assets vs USDC + adapter backing
// collector to the Prometheus registry (#1547).
func (m *Metrics) RegisterVaultSolvency(source VaultSolvencySource) error {
	if m == nil || source == nil {
		return nil
	}
	return m.registry.Register(newVaultSolvencyCollector(source))
}
