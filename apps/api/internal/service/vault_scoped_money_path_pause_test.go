package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/moneypath"
)

// vaultScopedPauseGate refuses one operation on exactly one vault, so a test
// can prove the per-vault switch is scoped and not a rebadged global halt.
type vaultScopedPauseGate struct {
	blockedVault uuid.UUID
	blockedOp    moneypath.Operation
}

func (g vaultScopedPauseGate) EnsureVaultAllowed(_ context.Context, vaultID uuid.UUID, op moneypath.Operation) error {
	if vaultID == g.blockedVault && op == g.blockedOp {
		return &moneypath.PausedError{Operation: op, Reason: "vault incident"}
	}
	return nil
}

// The per-vault switch has to stop the operation at the service boundary,
// not merely exist. This is the behaviour #1322 asks for.
func TestVaultPausedDepositIsRefusedByTheVaultService(t *testing.T) {
	vaultID := uuid.New()
	svc := NewVaultService(nil)
	svc.SetVaultMoneyPathSwitches(vaultScopedPauseGate{blockedVault: vaultID, blockedOp: moneypath.OperationDeposit})

	_, err := svc.RecordDeposit(context.Background(), RecordDepositInput{
		VaultID: vaultID,
		Amount:  decimal.NewFromInt(10),
	})

	if !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("deposit on a paused vault: got %v, want moneypath.ErrPaused", err)
	}
}

func TestVaultPausedWithdrawalIsRefusedByTheVaultService(t *testing.T) {
	vaultID := uuid.New()
	svc := NewVaultService(nil)
	svc.SetVaultMoneyPathSwitches(vaultScopedPauseGate{blockedVault: vaultID, blockedOp: moneypath.OperationWithdrawal})

	_, err := svc.RecordWithdrawal(context.Background(), RecordWithdrawalInput{
		VaultID: vaultID,
		Amount:  decimal.NewFromInt(10),
	})

	if !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("withdrawal on a paused vault: got %v, want moneypath.ErrPaused", err)
	}
}

// A rebalance reaches the chain, so a paused vault must not move funds
// through it either; bound to the withdrawal side, like the global switch.
func TestVaultPausedWithdrawalBlocksRebalance(t *testing.T) {
	vaultID := uuid.New()
	svc := NewVaultService(nil)
	svc.SetVaultMoneyPathSwitches(vaultScopedPauseGate{blockedVault: vaultID, blockedOp: moneypath.OperationWithdrawal})

	_, err := svc.RebalancePosition(context.Background(), RebalancePositionInput{
		VaultID:      vaultID,
		UserID:       uuid.New(),
		Amount:       decimal.NewFromInt(10),
		FromProtocol: "a",
		ToProtocol:   "b",
	})

	if !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("rebalance on a paused vault: got %v, want moneypath.ErrPaused", err)
	}
}

// The gate is scoped to one vault: every other vault, and the other
// operation on the same vault, must pass.
func TestVaultPauseLeavesOtherVaultsAndOperationsOpen(t *testing.T) {
	pausedVault := uuid.New()
	svc := NewVaultService(nil)
	svc.SetVaultMoneyPathSwitches(vaultScopedPauseGate{blockedVault: pausedVault, blockedOp: moneypath.OperationDeposit})

	if err := svc.ensureVaultMoneyPathAllowed(context.Background(), uuid.New(), moneypath.OperationDeposit); err != nil {
		t.Fatalf("another vault's deposit must be allowed: %v", err)
	}
	if err := svc.ensureVaultMoneyPathAllowed(context.Background(), pausedVault, moneypath.OperationWithdrawal); err != nil {
		t.Fatalf("the same vault's withdrawal must be allowed: %v", err)
	}
	if err := svc.ensureVaultMoneyPathAllowed(context.Background(), pausedVault, moneypath.OperationDeposit); !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("the paused vault's deposit should be refused, got %v", err)
	}
}

// A service with no per-vault gate is the configuration every existing test
// and tool uses, and must pass rather than refusing or panicking.
func TestVaultServiceWithoutScopedGateAllowsTheOperation(t *testing.T) {
	svc := NewVaultService(nil)

	if err := svc.ensureVaultMoneyPathAllowed(context.Background(), uuid.New(), moneypath.OperationDeposit); err != nil {
		t.Fatalf("a service with no per-vault gate must allow: %v", err)
	}
}
