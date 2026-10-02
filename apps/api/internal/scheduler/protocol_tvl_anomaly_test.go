package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/deterioration"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/protocoltvl"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

type anomalyFakeTVL struct{ tvl float64 }

func (f anomalyFakeTVL) ProtocolTVL(context.Context, string) (float64, error) { return f.tvl, nil }

// anomalyFakeTVLRepo returns a fixed latest snapshot and records inserts.
type anomalyFakeTVLRepo struct {
	mu       sync.Mutex
	latest   *protocoltvl.Snapshot
	inserted []float64
}

func (f *anomalyFakeTVLRepo) InsertSnapshot(_ context.Context, _ string, tvl float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inserted = append(f.inserted, tvl)
	return nil
}
func (f *anomalyFakeTVLRepo) SnapshotAt(context.Context, string, time.Time) (*protocoltvl.Snapshot, error) {
	return nil, nil
}
func (f *anomalyFakeTVLRepo) LatestSnapshot(context.Context, string) (*protocoltvl.Snapshot, error) {
	return f.latest, nil
}
func (f *anomalyFakeTVLRepo) ListSince(context.Context, string, time.Time) ([]protocoltvl.Snapshot, error) {
	return nil, nil
}
func (f *anomalyFakeTVLRepo) CanAlert(context.Context, string) (bool, error) { return true, nil }
func (f *anomalyFakeTVLRepo) RecordAlert(context.Context, string) error      { return nil }

func anomalyChecker(prior *protocoltvl.Snapshot, current, thresholdPct float64) (*ProtocolHealthChecker, *fakeDeteriorationRepo) {
	repo := &anomalyFakeTVLRepo{latest: prior}
	detRepo := &fakeDeteriorationRepo{}
	engine := NewDeteriorationEngine(repo, &fakeAPYLister{}, detRepo, nil, nil, nil)

	checker := NewProtocolHealthChecker(
		ProtocolHealthConfig{Enabled: true, Interval: time.Minute, AnomalyDropPct: thresholdPct},
		fakeActiveVaults{vaults: []vault.Vault{testVaultWithAllocation("aave")}},
		anomalyFakeTVL{tvl: current},
		repo,
		noopHealthNotifier{},
		nil,
	)
	checker.SetDeteriorationEngine(engine)
	return checker, detRepo
}

func TestProtocolHealthChecker_TVLAnomalySurfacedToDeteriorationPipeline(t *testing.T) {
	prior := &protocoltvl.Snapshot{TVLUSD: 1_000_000, SnapshottedAt: time.Now().Add(-30 * time.Minute)}
	checker, detRepo := anomalyChecker(prior, 700_000, 10)

	checker.Tick(context.Background())

	actions, _ := detRepo.ListActionsByProtocol(context.Background(), "aave", 10)
	var found bool
	for _, a := range actions {
		if a.Kind == deterioration.ActionRecommendRebalance && a.Level == deterioration.LevelModerate {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a moderate recommend_rebalance action for the anomaly, got %+v", actions)
	}
}

func TestProtocolHealthChecker_SmallDropIsNotAnAnomaly(t *testing.T) {
	prior := &protocoltvl.Snapshot{TVLUSD: 1_000_000, SnapshottedAt: time.Now().Add(-30 * time.Minute)}
	checker, detRepo := anomalyChecker(prior, 950_000, 10)

	checker.Tick(context.Background())

	actions, _ := detRepo.ListActionsByProtocol(context.Background(), "aave", 10)
	for _, a := range actions {
		if a.Kind == deterioration.ActionRecommendRebalance {
			t.Fatalf("5%% drop must not be flagged at a 10%% threshold, got %+v", a)
		}
	}
}

func TestProtocolHealthChecker_AnomalyThresholdIsConfigurable(t *testing.T) {
	prior := &protocoltvl.Snapshot{TVLUSD: 1_000_000, SnapshottedAt: time.Now().Add(-30 * time.Minute)}
	// 5% drop trips a 3% threshold.
	checker, detRepo := anomalyChecker(prior, 950_000, 3)

	checker.Tick(context.Background())

	if n := detRepo.actionCount(); n == 0 {
		t.Fatal("expected the 5% drop to be flagged at a 3% threshold")
	}
}

func TestProtocolHealthChecker_NoPriorSnapshotNoAnomaly(t *testing.T) {
	checker, detRepo := anomalyChecker(nil, 1, 10)

	checker.Tick(context.Background())

	if n := detRepo.actionCount(); n != 0 {
		t.Fatalf("expected no actions without a prior snapshot, got %d", n)
	}
}

func TestDeteriorationEngine_HandleTVLAnomaly_RecommendsWithoutMovingFunds(t *testing.T) {
	repo := &fakeDeteriorationRepo{}
	rebalancer := &fakeRebalanceTrigger{}
	engine := NewDeteriorationEngine(&fakeTVLRepo{}, &fakeAPYLister{}, repo, rebalancer, nil, nil)

	engine.HandleTVLAnomaly(context.Background(), protocoltvl.Anomaly{
		ProtocolSlug:   "aave",
		PreviousTVLUSD: 100,
		CurrentTVLUSD:  50,
		DropPct:        50,
		ThresholdPct:   10,
		DetectedAt:     time.Now(),
	})

	if len(repo.assessments) != 1 {
		t.Fatalf("expected 1 recorded assessment, got %d", len(repo.assessments))
	}
	if got := repo.assessments[0]; got.Level != deterioration.LevelModerate || got.Indicators.TVLOutflowVelocityPct != 50 {
		t.Errorf("unexpected assessment: %+v", got)
	}
	if repo.actionCount() != 1 {
		t.Fatalf("expected 1 audited action, got %d", repo.actionCount())
	}
	if rebalancer.callCount() != 0 {
		t.Errorf("an anomaly alone must not trigger an automatic rebalance, got %d calls", rebalancer.callCount())
	}
}
