package scheduler

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stellar/go/xdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/suncrestlabs/nester/apps/api/internal/metrics"
)

type mockTTLKeeperChain struct {
	mu           sync.Mutex
	latestLedger uint32
	entries      map[string]*EntryTTLInfo
	extendCalls  []uint32
}

func newMockTTLKeeperChain(latestLedger uint32) *mockTTLKeeperChain {
	return &mockTTLKeeperChain{
		latestLedger: latestLedger,
		entries:      make(map[string]*EntryTTLInfo),
	}
}

func (m *mockTTLKeeperChain) GetLatestLedger(ctx context.Context) (uint32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.latestLedger, nil
}

func (m *mockTTLKeeperChain) BuildMonitoredKeys(ctx context.Context, contractAddresses []string, userAddresses []string) ([]xdr.LedgerKey, error) {
	return BuildDefaultMonitoredKeys(contractAddresses, userAddresses)
}

func (m *mockTTLKeeperChain) GetLedgerEntriesTTL(ctx context.Context, keys []xdr.LedgerKey) ([]EntryTTLInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []EntryTTLInfo
	for _, k := range keys {
		b, _ := k.MarshalBinary()
		keyStr := string(b)
		if entry, ok := m.entries[keyStr]; ok {
			result = append(result, *entry)
		} else {
			// Default entry if not explicitly set
			result = append(result, EntryTTLInfo{
				Key:                k,
				ContractAddress:    "CAAQCAIBAEAQCAIBAEAQCAIBAEAQCAIBAEAQCAIBAEAQCAIBAEAQC526",
				Kind:               "instance",
				LiveUntilLedgerSeq: m.latestLedger + 500_000,
			})
		}
	}
	return result, nil
}

func (m *mockTTLKeeperChain) ExtendTTL(ctx context.Context, keys []xdr.LedgerKey, extendTo uint32) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.extendCalls = append(m.extendCalls, extendTo)

	// Update the liveUntilLedgerSeq for extended keys
	for _, k := range keys {
		b, _ := k.MarshalBinary()
		keyStr := string(b)
		if entry, ok := m.entries[keyStr]; ok {
			entry.LiveUntilLedgerSeq = extendTo
		}
	}
	return "mock_tx_hash_12345", nil
}

func readGaugeValue(t *testing.T, m *metrics.Metrics, contract, kind string) float64 {
	mfs, err := m.Registry().Gather()
	require.NoError(t, err)

	for _, mf := range mfs {
		if mf.GetName() == "nester_contract_entry_ttl_ledgers_remaining" {
			for _, metric := range mf.GetMetric() {
				var matchContract, matchKind bool
				for _, label := range metric.GetLabel() {
					if label.GetName() == "contract" && label.GetValue() == contract {
						matchContract = true
					}
					if label.GetName() == "kind" && label.GetValue() == kind {
						matchKind = true
					}
				}
				if matchContract && matchKind {
					return metric.GetGauge().GetValue()
				}
			}
		}
	}
	t.Fatalf("metric nester_contract_entry_ttl_ledgers_remaining not found for contract=%s kind=%s", contract, kind)
	return 0
}

func TestTTLKeeper_NonLeaderSkips(t *testing.T) {
	m := metrics.New()
	chain := newMockTTLKeeperChain(100_000)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cfg := TTLKeeperConfig{
		Enabled:                true,
		ExpiryThresholdLedgers: 120_960,
		ExtendToLedgers:        518_400,
		ContractAddresses:      []string{"CAAQCAIBAEAQCAIBAEAQCAIBAEAQCAIBAEAQCAIBAEAQCAIBAEAQC526"},
	}

	job := NewTTLKeeperJob(cfg, chain, m, logger)
	job.SetLeaderChecker(fakeLeaderChecker{leader: false})

	count, err := job.Step(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Empty(t, chain.extendCalls)
}

func TestTTLKeeper_ExtendsExpiringEntriesAndRisesMetric(t *testing.T) {
	m := metrics.New()
	currentLedger := uint32(100_000)
	chain := newMockTTLKeeperChain(currentLedger)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	contractAddr := "CAAQCAIBAEAQCAIBAEAQCAIBAEAQCAIBAEAQCAIBAEAQCAIBAEAQC526"
	keys, err := BuildDefaultMonitoredKeys([]string{contractAddr}, nil)
	require.NoError(t, err)
	require.Len(t, keys, 1)

	instanceKey := keys[0]
	b, _ := instanceKey.MarshalBinary()
	keyStr := string(b)

	// Set low TTL: remaining = 50,000 ledgers (< threshold of 120,960)
	chain.entries[keyStr] = &EntryTTLInfo{
		Key:                instanceKey,
		ContractAddress:    contractAddr,
		Kind:               "instance",
		LiveUntilLedgerSeq: currentLedger + 50_000,
	}

	cfg := TTLKeeperConfig{
		Enabled:                true,
		Interval:               1 * time.Hour,
		ExpiryThresholdLedgers: 120_960,
		ExtendToLedgers:        518_400,
		ContractAddresses:      []string{contractAddr},
	}

	job := NewTTLKeeperJob(cfg, chain, m, logger)
	job.SetLeaderChecker(fakeLeaderChecker{leader: true})

	// Run step: entry should be detected as expiring and extended
	extendedCount, err := job.Step(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, extendedCount)
	require.Len(t, chain.extendCalls, 1)

	expectedExtendTo := currentLedger + cfg.ExtendToLedgers
	assert.Equal(t, expectedExtendTo, chain.extendCalls[0])

	// Verify metric shows TTL rising after run
	remainingAfter := readGaugeValue(t, m, contractAddr, "instance")
	assert.Equal(t, float64(cfg.ExtendToLedgers), remainingAfter)
	assert.Greater(t, remainingAfter, float64(cfg.ExpiryThresholdLedgers))
}

func TestTTLKeeper_HealthyEntriesNotExtended(t *testing.T) {
	m := metrics.New()
	currentLedger := uint32(100_000)
	chain := newMockTTLKeeperChain(currentLedger)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	contractAddr := "CAAQCAIBAEAQCAIBAEAQCAIBAEAQCAIBAEAQCAIBAEAQCAIBAEAQC526"
	keys, err := BuildDefaultMonitoredKeys([]string{contractAddr}, nil)
	require.NoError(t, err)

	instanceKey := keys[0]
	b, _ := instanceKey.MarshalBinary()
	keyStr := string(b)

	// Set healthy TTL: remaining = 400,000 ledgers (> threshold of 120,960)
	chain.entries[keyStr] = &EntryTTLInfo{
		Key:                instanceKey,
		ContractAddress:    contractAddr,
		Kind:               "instance",
		LiveUntilLedgerSeq: currentLedger + 400_000,
	}

	cfg := TTLKeeperConfig{
		Enabled:                true,
		ExpiryThresholdLedgers: 120_960,
		ExtendToLedgers:        518_400,
		ContractAddresses:      []string{contractAddr},
	}

	job := NewTTLKeeperJob(cfg, chain, m, logger)
	job.SetLeaderChecker(fakeLeaderChecker{leader: true})

	extendedCount, err := job.Step(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, extendedCount)
	assert.Empty(t, chain.extendCalls)

	// Metric still records the healthy TTL
	remaining := readGaugeValue(t, m, contractAddr, "instance")
	assert.Equal(t, float64(400_000), remaining)
}
