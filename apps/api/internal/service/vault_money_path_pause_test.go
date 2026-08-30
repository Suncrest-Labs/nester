package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/moneypath"
)

// pausedGate refuses one operation and allows the other, so a test can assert
// the two switches really are independent at the service boundary.
type pausedGate struct{ blocked moneypath.Operation }

func (g pausedGate) EnsureAllowed(_ context.Context, op moneypath.Operation) error {
	if op == g.blocked {
		return &moneypath.PausedError{Operation: op, Reason: "incident 1234"}
	}
	return nil
}

// The switch has to stop the operation at the service boundary, not merely
// exist. These assert the money path itself refuses, which is the behaviour
// #1120 is asking for.
func TestPausedDepositIsRefusedByTheVaultService(t *testing.T) {
	svc := NewVaultService(nil)
	svc.SetMoneyPathSwitches(pausedGate{blocked: moneypath.OperationDeposit})

	_, err := svc.RecordDeposit(context.Background(), RecordDepositInput{
		VaultID: uuid.New(),
		Amount:  decimal.NewFromInt(10),
	})

	if !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("deposit during a pause: got %v, want moneypath.ErrPaused", err)
	}
}

func TestPausedWithdrawalIsRefusedByTheVaultService(t *testing.T) {
	svc := NewVaultService(nil)
	svc.SetMoneyPathSwitches(pausedGate{blocked: moneypath.OperationWithdrawal})

	_, err := svc.RecordWithdrawal(context.Background(), RecordWithdrawalInput{
		VaultID: uuid.New(),
		Amount:  decimal.NewFromInt(10),
	})

	if !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("withdrawal during a pause: got %v, want moneypath.ErrPaused", err)
	}
}

// Pausing deposits must not stop withdrawals: letting users take their money
// out while new money is halted is the whole point of separate switches.
func TestPausingDepositsDoesNotBlockWithdrawalsEndToEnd(t *testing.T) {
	svc := NewVaultService(nil)
	svc.SetMoneyPathSwitches(pausedGate{blocked: moneypath.OperationDeposit})

	if err := svc.ensureMoneyPathAllowed(context.Background(), moneypath.OperationWithdrawal); err != nil {
		t.Fatalf("withdrawals must stay open while only deposits are paused: %v", err)
	}
	if err := svc.ensureMoneyPathAllowed(context.Background(), moneypath.OperationDeposit); !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("deposits should be refused, got %v", err)
	}
}

// A service with no gate is the configuration every existing test and tool
// uses, and must pass the gate rather than refusing or panicking.
func TestVaultServiceWithoutAGateAllowsTheOperation(t *testing.T) {
	svc := NewVaultService(nil)

	if err := svc.ensureMoneyPathAllowed(context.Background(), moneypath.OperationDeposit); err != nil {
		t.Fatalf("a service with no pause gate must allow: %v", err)
	}
	if err := svc.ensureMoneyPathAllowed(context.Background(), moneypath.OperationWithdrawal); err != nil {
		t.Fatalf("a service with no pause gate must allow: %v", err)
	}
}

// A rebalance moves funds between protocols and reaches the chain, so an
// engaged switch has to stop it too. Gating only deposits and withdrawals
// left this path open while the money path was supposed to be halted.
func TestPausedWithdrawalAlsoBlocksRebalance(t *testing.T) {
	svc := NewVaultService(nil)
	svc.SetMoneyPathSwitches(pausedGate{blocked: moneypath.OperationWithdrawal})

	_, err := svc.RebalancePosition(context.Background(), RebalancePositionInput{
		VaultID:      uuid.New(),
		UserID:       uuid.New(),
		Amount:       decimal.NewFromInt(10),
		FromProtocol: "a",
		ToProtocol:   "b",
	})

	if !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("rebalance during a withdrawal pause: got %v, want moneypath.ErrPaused", err)
	}
}

// EmergencyWithdraw stays ungated on purpose: blocking the emergency exit
// during an incident is backwards. This pins that as a decision rather than
// letting it look like the same oversight rebalance was.
func TestPausedWithdrawalDoesNotBlockEmergencyWithdraw(t *testing.T) {
	svc := NewVaultService(nil)
	svc.SetMoneyPathSwitches(pausedGate{blocked: moneypath.OperationWithdrawal})

	_, err := svc.EmergencyWithdraw(context.Background(), EmergencyWithdrawInput{
		VaultID: uuid.Nil,
	})

	if errors.Is(err, moneypath.ErrPaused) {
		t.Fatal("emergency withdraw must not be blocked by the pause switch")
	}
}
