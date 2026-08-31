package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	admindomain "github.com/suncrestlabs/nester/apps/api/internal/domain/admin"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/apysnapshot"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/deterioration"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/protocoltvl"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

type fakeTVLRepo struct {
	snaps map[string][]protocoltvl.Snapshot
}

func (f *fakeTVLRepo) InsertSnapshot(context.Context, string, float64) error { return nil }
func (f *fakeTVLRepo) SnapshotAt(context.Context, string, time.Time) (*protocoltvl.Snapshot, error) {
	return nil, nil
}
func (f *fakeTVLRepo) LatestSnapshot(context.Context, string) (*protocoltvl.Snapshot, error) {
	return nil, nil
}
func (f *fakeTVLRepo) ListSince(_ context.Context, slug string, _ time.Time) ([]protocoltvl.Snapshot, error) {
	return f.snaps[slug], nil
}
func (f *fakeTVLRepo) CanAlert(context.Context, string) (bool, error) { return true, nil }
func (f *fakeTVLRepo) RecordAlert(context.Context, string) error      { return nil }

type fakeAPYLister struct {
	snaps map[string][]apysnapshot.APYSnapshot
}

func (f *fakeAPYLister) ListByProtocol(_ context.Context, slug string, _ time.Time) ([]apysnapshot.APYSnapshot, error) {
	return f.snaps[slug], nil
}

type fakeDeteriorationRepo struct {
	mu          sync.Mutex
	actions     []deterioration.Action
	assessments []deterioration.Assessment
}

func (f *fakeDeteriorationRepo) RecordAction(_ context.Context, a *deterioration.Action) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	f.actions = append(f.actions, *a)
	return nil
}
func (f *fakeDeteriorationRepo) ListActionsByProtocol(_ context.Context, slug string, _ int) ([]deterioration.Action, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []deterioration.Action
	for _, a := range f.actions {
		if a.ProtocolSlug == slug {
			out = append(out, a)
		}
	}
	return out, nil
}
func (f *fakeDeteriorationRepo) RecordAssessment(_ context.Context, a deterioration.Assessment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assessments = append(f.assessments, a)
	return nil
}

func (f *fakeDeteriorationRepo) actionCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.actions)
}

type fakeRebalanceTrigger struct {
	mu       sync.Mutex
	calls    []uuid.UUID
	response admindomain.RebalanceResponse
	err      error
}

func (f *fakeRebalanceTrigger) TriggerRebalance(_ context.Context, vaultID uuid.UUID, _ admindomain.RebalanceRequest) (admindomain.RebalanceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, vaultID)
	if f.err != nil {
		return admindomain.RebalanceResponse{}, f.err
	}
	return f.response, nil
}

func (f *fakeRebalanceTrigger) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func testVaultWithAllocation(protocol string) vault.Vault {
	return vault.Vault{
		ID:     uuid.New(),
		UserID: uuid.New(),
		Allocations: []vault.Allocation{
			{Protocol: protocol, Amount: decimal.NewFromInt(1000), APY: decimal.NewFromFloat(5.0)},
		},
	}
}

func TestDeteriorationEngine_Assess_RecordsAssessment(t *testing.T) {
	tvlRepo := &fakeTVLRepo{snaps: map[string][]protocoltvl.Snapshot{
		"aave": tvlSeries(1_000_000, 900_000, 800_000, 700_000),
	}}
	apyLister := &fakeAPYLister{}
	repo := &fakeDeteriorationRepo{}
	engine := NewDeteriorationEngine(tvlRepo, apyLister, repo, nil, nil, nil)

	assessment, err := engine.Assess(context.Background(), "aave")
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if assessment.ProtocolSlug != "aave" {
		t.Errorf("unexpected protocol slug: %q", assessment.ProtocolSlug)
	}
	if len(repo.assessments) != 1 {
		t.Fatalf("expected 1 recorded assessment, got %d", len(repo.assessments))
	}
}

func TestDeteriorationEngine_DispatchAction_MildRecordsCeilingCut(t *testing.T) {
	repo := &fakeDeteriorationRepo{}
	engine := NewDeteriorationEngine(&fakeTVLRepo{}, &fakeAPYLister{}, repo, nil, nil, nil)

	assessment := deterioration.Assessment{ProtocolSlug: "aave", Level: deterioration.LevelMild, Probability: 0.35}
	engine.DispatchAction(context.Background(), assessment, nil)

	if repo.actionCount() != 1 {
		t.Fatalf("expected 1 recorded action, got %d", repo.actionCount())
	}
	if repo.actions[0].Kind != deterioration.ActionCeilingCut {
		t.Errorf("expected ceiling_cut action, got %q", repo.actions[0].Kind)
	}
}

func TestDeteriorationEngine_DispatchAction_ModerateRecommendsRebalanceWithoutMovingFunds(t *testing.T) {
	repo := &fakeDeteriorationRepo{}
	rebalancer := &fakeRebalanceTrigger{}
	engine := NewDeteriorationEngine(&fakeTVLRepo{}, &fakeAPYLister{}, repo, rebalancer, nil, nil)

	assessment := deterioration.Assessment{ProtocolSlug: "aave", Level: deterioration.LevelModerate, Probability: 0.6}
	engine.DispatchAction(context.Background(), assessment, []vault.Vault{testVaultWithAllocation("aave")})

	if rebalancer.callCount() != 0 {
		t.Errorf("expected moderate level to NOT trigger automatic rebalance, got %d calls", rebalancer.callCount())
	}
	if repo.actionCount() != 1 || repo.actions[0].Kind != deterioration.ActionRecommendRebalance {
		t.Fatalf("expected 1 recommend_rebalance action, got %+v", repo.actions)
	}
}

func TestDeteriorationEngine_DispatchAction_SevereTriggersAutomaticRebalanceForAffectedVaultsOnly(t *testing.T) {
	repo := &fakeDeteriorationRepo{}
	rebalanceID := uuid.New()
	rebalancer := &fakeRebalanceTrigger{response: admindomain.RebalanceResponse{Status: "submitted", RebalanceID: rebalanceID}}
	engine := NewDeteriorationEngine(&fakeTVLRepo{}, &fakeAPYLister{}, repo, rebalancer, nil, nil)

	affected := testVaultWithAllocation("aave")
	unaffected := testVaultWithAllocation("compound")
	assessment := deterioration.Assessment{ProtocolSlug: "aave", Level: deterioration.LevelSevere, Probability: 0.9}

	engine.DispatchAction(context.Background(), assessment, []vault.Vault{affected, unaffected})

	if rebalancer.callCount() != 1 {
		t.Fatalf("expected exactly 1 automatic rebalance (only the affected vault), got %d", rebalancer.callCount())
	}
	if rebalancer.calls[0] != affected.ID {
		t.Errorf("expected the rebalance to target the affected vault %s, got %s", affected.ID, rebalancer.calls[0])
	}

	// Auditability: the automatic action must be logged with the rebalance id.
	if repo.actionCount() != 1 {
		t.Fatalf("expected 1 audited action, got %d", repo.actionCount())
	}
	audited := repo.actions[0]
	if audited.Kind != deterioration.ActionAutomaticRebalance {
		t.Errorf("expected automatic_rebalance kind, got %q", audited.Kind)
	}
	if audited.VaultID == nil || *audited.VaultID != affected.ID {
		t.Errorf("expected the audited action to reference the affected vault, got %+v", audited.VaultID)
	}
	if audited.RebalanceID == nil || *audited.RebalanceID != rebalanceID {
		t.Errorf("expected the audited action to reference the rebalance id, got %+v", audited.RebalanceID)
	}
}

func TestDeteriorationEngine_DispatchAction_SevereRebalanceFailureIsStillAudited(t *testing.T) {
	// #857: automatic capital movement must never be silent — including
	// when the attempt fails.
	repo := &fakeDeteriorationRepo{}
	rebalancer := &fakeRebalanceTrigger{err: errors.New("simulation failed")}
	engine := NewDeteriorationEngine(&fakeTVLRepo{}, &fakeAPYLister{}, repo, rebalancer, nil, nil)

	affected := testVaultWithAllocation("aave")
	assessment := deterioration.Assessment{ProtocolSlug: "aave", Level: deterioration.LevelSevere, Probability: 0.95}
	engine.DispatchAction(context.Background(), assessment, []vault.Vault{affected})

	if repo.actionCount() != 1 {
		t.Fatalf("expected the failed attempt to still be audited, got %d actions", repo.actionCount())
	}
	if repo.actions[0].Error == "" {
		t.Error("expected the audited action to record the failure error")
	}
}

func TestDeteriorationEngine_DispatchAction_NoneLevelTakesNoAction(t *testing.T) {
	repo := &fakeDeteriorationRepo{}
	rebalancer := &fakeRebalanceTrigger{}
	engine := NewDeteriorationEngine(&fakeTVLRepo{}, &fakeAPYLister{}, repo, rebalancer, nil, nil)

	assessment := deterioration.Assessment{ProtocolSlug: "aave", Level: deterioration.LevelNone, Probability: 0.05}
	engine.DispatchAction(context.Background(), assessment, []vault.Vault{testVaultWithAllocation("aave")})

	if repo.actionCount() != 0 || rebalancer.callCount() != 0 {
		t.Errorf("expected no action for level=none, got %d actions, %d rebalance calls", repo.actionCount(), rebalancer.callCount())
	}
}
