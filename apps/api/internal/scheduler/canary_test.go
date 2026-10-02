package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeCanaryInvoker is a test double for CanaryInvoker.
type fakeCanaryInvoker struct {
	depositErr  error
	withdrawErr error
	calls       int
}

func (f *fakeCanaryInvoker) CanaryDeposit(_ context.Context, _ uuid.UUID, _ string) (string, error) {
	f.calls++
	if f.depositErr != nil {
		return "", f.depositErr
	}
	return "deposit-tx-hash", nil
}

func (f *fakeCanaryInvoker) CanaryWithdraw(_ context.Context, _ uuid.UUID, _ string) (string, error) {
	if f.withdrawErr != nil {
		return "", f.withdrawErr
	}
	return "withdraw-tx-hash", nil
}

func TestCanaryJob_DisabledDoesNotRun(t *testing.T) {
	invoker := &fakeCanaryInvoker{}
	job := NewCanaryJob(CanaryConfig{Enabled: false}, invoker, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	job.Run(ctx)
	if invoker.calls != 0 {
		t.Errorf("expected 0 canary invocations when disabled, got %d", invoker.calls)
	}
}

func TestCanaryJob_RoundTripSuccess(t *testing.T) {
	invoker := &fakeCanaryInvoker{}
	job := NewCanaryJob(CanaryConfig{
		Enabled:          true,
		Interval:         10 * time.Hour, // prevent a second tick during the test
		VaultID:          uuid.New(),
		Amount:           "0.01",
		LatencyThreshold: 30 * time.Second,
	}, invoker, nil, nil)

	depositHash, withdrawHash, err := job.roundTrip(context.Background())
	if err != nil {
		t.Fatalf("expected round trip to succeed, got: %v", err)
	}
	if depositHash != "deposit-tx-hash" {
		t.Errorf("expected deposit-tx-hash, got %q", depositHash)
	}
	if withdrawHash != "withdraw-tx-hash" {
		t.Errorf("expected withdraw-tx-hash, got %q", withdrawHash)
	}
}

func TestCanaryJob_DepositFailureIsReported(t *testing.T) {
	invoker := &fakeCanaryInvoker{depositErr: errors.New("rpc timeout")}
	job := NewCanaryJob(CanaryConfig{
		Enabled:  true,
		Interval: 10 * time.Hour,
		VaultID:  uuid.New(),
		Amount:   "0.01",
	}, invoker, nil, nil)

	_, _, err := job.roundTrip(context.Background())
	if err == nil {
		t.Fatal("expected round trip to fail when deposit errors")
	}
}

func TestCanaryJob_WithdrawFailureIsReported(t *testing.T) {
	invoker := &fakeCanaryInvoker{withdrawErr: errors.New("contract error #7")}
	job := NewCanaryJob(CanaryConfig{
		Enabled:  true,
		Interval: 10 * time.Hour,
		VaultID:  uuid.New(),
		Amount:   "0.01",
	}, invoker, nil, nil)

	depositHash, _, err := job.roundTrip(context.Background())
	if err == nil {
		t.Fatal("expected round trip to fail when withdrawal errors")
	}
	// deposit still returned its hash even though the withdrawal failed
	if depositHash != "deposit-tx-hash" {
		t.Errorf("expected deposit hash on partial failure, got %q", depositHash)
	}
}

func TestCanaryJob_NonLeaderSkipsProbe(t *testing.T) {
	invoker := &fakeCanaryInvoker{}
	job := NewCanaryJob(CanaryConfig{
		Enabled:  true,
		Interval: 10 * time.Hour,
		VaultID:  uuid.New(),
		Amount:   "0.01",
	}, invoker, nil, nil)
	job.SetLeaderChecker(neverLeader{})
	job.probe(context.Background())
	if invoker.calls != 0 {
		t.Errorf("non-leader should not execute canary probe, got %d calls", invoker.calls)
	}
}

// neverLeader satisfies LeaderChecker and always reports not-leader.
type neverLeader struct{}

func (neverLeader) IsLeader() bool { return false }
