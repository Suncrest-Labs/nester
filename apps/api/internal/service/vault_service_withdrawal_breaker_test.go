package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

func TestWithdrawalCircuitBreakerHaltsVault(t *testing.T) {
	userID := uuid.New()
	repo := newMemoryVaultRepository(userID)
	svc := NewVaultService(repo)
	// The breaker defaults to disabled (nester#1377); turn it on explicitly
	// for this test rather than relying on the zero-config default.
	svc.SetOutflowBreakerConfig(vault.OutflowBreakerConfig{
		Enabled:          true,
		ThresholdPercent: decimal.NewFromInt(25),
		Window:           time.Hour,
	})
	svc.SetChainEventVerifier(&fakeChainVerifier{events: map[string]VerifiedVaultEvent{
		"hash-deposit": {Amount: decimal.RequireFromString("100"), EventType: "deposit", ContractID: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		"hash-1":       {Amount: decimal.RequireFromString("30"), EventType: "withdraw", ContractID: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
	}})

	created, err := svc.CreateVault(context.Background(), CreateVaultInput{
		UserID: userID, ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Currency: "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault: %v", err)
	}

	_, err = svc.RecordDeposit(context.Background(), RecordDepositInput{
		VaultID: created.ID, Amount: decimal.RequireFromString("100"), TxHash: "hash-deposit",
	})
	if err != nil {
		t.Fatalf("RecordDeposit: %v", err)
	}

	// Withdraw 30% of TVL within the window (Threshold is 25%)
	_, err = svc.RecordWithdrawal(context.Background(), RecordWithdrawalInput{
		VaultID: created.ID, Amount: decimal.RequireFromString("30"), TxHash: "hash-1",
	})
	if err != nil && err != vault.ErrVaultPausedByBreaker {
		t.Fatalf("RecordWithdrawal unexpected error: %v", err)
	}

	vaultModel, err := svc.GetVault(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetVault: %v", err)
	}
	if vaultModel.Status != vault.StatusPaused {
		t.Fatal("expected vault to be paused by withdrawal circuit breaker when outflows exceed threshold")
	}
}
