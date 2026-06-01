package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type fakeVaults struct {
	vaults []VaultSnapshot
	err    error
}

func (f fakeVaults) ListActiveVaults(_ context.Context) ([]VaultSnapshot, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.vaults, nil
}

type fakeYields struct {
	yields []ProtocolYield
	err    error
}

func (f fakeYields) FetchYields(_ context.Context) ([]ProtocolYield, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.yields, nil
}

type recordingSubmitter struct {
	mu    sync.Mutex
	calls []recordedCall
	err   error
}

type recordedCall struct {
	VaultID  uuid.UUID
	Protocol string
	GainBPS  int64
}

func (r *recordingSubmitter) SubmitRebalance(_ context.Context, id uuid.UUID, protocol string, gain int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordedCall{id, protocol, gain})
	return r.err
}

func TestTriggerOnce_RebalancesWhenGainExceedsThreshold(t *testing.T) {
	vault := VaultSnapshot{
		ID:              uuid.New(),
		ContractAddress: "CABC",
		CurrentAllocations: []CurrentAllocation{
			{Protocol: "blend", Amount: decimal.NewFromInt(1000)},
		},
	}
	subm := &recordingSubmitter{}
	s := New(
		Config{Enabled: true, MinAPYGainBPS: 50},
		fakeVaults{vaults: []VaultSnapshot{vault}},
		fakeYields{yields: []ProtocolYield{
			{Protocol: "blend", APY: decimal.RequireFromString("0.05")},
			{Protocol: "aave", APY: decimal.RequireFromString("0.07")}, // +200 BPS
		}},
		subm,
		nil,
	)

	if err := s.TriggerOnce(context.Background(), uuid.Nil); err != nil {
		t.Fatalf("TriggerOnce: %v", err)
	}
	if len(subm.calls) != 1 {
		t.Fatalf("expected 1 submit, got %d", len(subm.calls))
	}
	if subm.calls[0].VaultID != vault.ID || subm.calls[0].Protocol != "aave" || subm.calls[0].GainBPS != 200 {
		t.Errorf("unexpected submit: %+v", subm.calls[0])
	}
}

func TestTriggerOnce_SkipsWhenGainBelowThreshold(t *testing.T) {
	vault := VaultSnapshot{
		ID: uuid.New(),
		CurrentAllocations: []CurrentAllocation{
			{Protocol: "blend", Amount: decimal.NewFromInt(1000)},
		},
	}
	subm := &recordingSubmitter{}
	s := New(
		Config{Enabled: true, MinAPYGainBPS: 50},
		fakeVaults{vaults: []VaultSnapshot{vault}},
		fakeYields{yields: []ProtocolYield{
			{Protocol: "blend", APY: decimal.RequireFromString("0.05")},
			{Protocol: "aave", APY: decimal.RequireFromString("0.054")}, // +40 BPS, below threshold
		}},
		subm,
		nil,
	)

	if err := s.TriggerOnce(context.Background(), uuid.Nil); err != nil {
		t.Fatalf("TriggerOnce: %v", err)
	}
	if len(subm.calls) != 0 {
		t.Errorf("expected 0 submits when gain < threshold, got %d", len(subm.calls))
	}
}

func TestTriggerOnce_FiltersByVaultID(t *testing.T) {
	vaultA := VaultSnapshot{ID: uuid.New(), CurrentAllocations: []CurrentAllocation{{Protocol: "blend", Amount: decimal.NewFromInt(1000)}}}
	vaultB := VaultSnapshot{ID: uuid.New(), CurrentAllocations: []CurrentAllocation{{Protocol: "blend", Amount: decimal.NewFromInt(1000)}}}
	subm := &recordingSubmitter{}
	s := New(
		Config{Enabled: true, MinAPYGainBPS: 50},
		fakeVaults{vaults: []VaultSnapshot{vaultA, vaultB}},
		fakeYields{yields: []ProtocolYield{
			{Protocol: "blend", APY: decimal.RequireFromString("0.05")},
			{Protocol: "aave", APY: decimal.RequireFromString("0.07")},
		}},
		subm,
		nil,
	)

	if err := s.TriggerOnce(context.Background(), vaultB.ID); err != nil {
		t.Fatalf("TriggerOnce: %v", err)
	}
	if len(subm.calls) != 1 || subm.calls[0].VaultID != vaultB.ID {
		t.Errorf("expected single submit for vault B only; got %+v", subm.calls)
	}
}

func TestTriggerOnce_CircuitBreakerErrorIsTolerated(t *testing.T) {
	vault := VaultSnapshot{ID: uuid.New(), CurrentAllocations: []CurrentAllocation{{Protocol: "blend", Amount: decimal.NewFromInt(1000)}}}
	subm := &recordingSubmitter{err: ErrCircuitBreakerTriggered}
	s := New(
		Config{Enabled: true, MinAPYGainBPS: 50},
		fakeVaults{vaults: []VaultSnapshot{vault}},
		fakeYields{yields: []ProtocolYield{
			{Protocol: "blend", APY: decimal.RequireFromString("0.05")},
			{Protocol: "aave", APY: decimal.RequireFromString("0.07")},
		}},
		subm,
		nil,
	)

	// TriggerOnce must NOT propagate the circuit-breaker sentinel — it
	// is per-vault and the next tick should keep trying.
	if err := s.TriggerOnce(context.Background(), uuid.Nil); err != nil {
		t.Fatalf("circuit breaker should not surface as a Trigger error, got %v", err)
	}
	if len(subm.calls) != 1 {
		t.Fatalf("expected the submitter to have been called once, got %d", len(subm.calls))
	}
}

func TestTriggerOnce_PropagatesUpstreamErrors(t *testing.T) {
	subm := &recordingSubmitter{}
	wantErr := errors.New("postgres down")
	s := New(
		Config{Enabled: true, MinAPYGainBPS: 50},
		fakeVaults{err: wantErr},
		fakeYields{},
		subm,
		nil,
	)
	if err := s.TriggerOnce(context.Background(), uuid.Nil); !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped vault list error, got %v", err)
	}
}

func TestRun_NoOpsWhenDisabled(t *testing.T) {
	subm := &recordingSubmitter{}
	s := New(
		Config{Enabled: false, MinAPYGainBPS: 50},
		fakeVaults{vaults: []VaultSnapshot{{ID: uuid.New()}}},
		fakeYields{yields: []ProtocolYield{{Protocol: "aave", APY: decimal.RequireFromString("0.07")}}},
		subm,
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // returns immediately because Enabled=false
	s.Run(ctx)

	if len(subm.calls) != 0 {
		t.Errorf("disabled scheduler should never submit; got %d submits", len(subm.calls))
	}
}
