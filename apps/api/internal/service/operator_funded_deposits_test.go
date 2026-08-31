package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// operatorBalanceInvoker models the shared operator account: a server-submitted
// deposit debits it, exactly as the contract's deposit(operator, operator, ...)
// does on chain.
type operatorBalanceInvoker struct {
	operatorBalance decimal.Decimal
	depositCalls    int
}

func newOperatorBalanceInvoker(start string) *operatorBalanceInvoker {
	return &operatorBalanceInvoker{operatorBalance: decimal.RequireFromString(start)}
}

func (o *operatorBalanceInvoker) DepositToVault(_ context.Context, _ string, amountStroops int64) error {
	o.depositCalls++
	o.operatorBalance = o.operatorBalance.Sub(
		decimal.NewFromInt(amountStroops).Div(decimal.NewFromInt(10_000_000)),
	)
	return nil
}

func (o *operatorBalanceInvoker) WithdrawFromVault(context.Context, string, int64, int) (string, error) {
	return "hash", nil
}

func (o *operatorBalanceInvoker) PreviewDeposit(context.Context, string, int64) (int64, error) {
	return 0, nil
}

func (o *operatorBalanceInvoker) PreviewWithdraw(context.Context, string, int64) (int64, error) {
	return 0, nil
}

func (o *operatorBalanceInvoker) HarvestVault(context.Context, string, string, bool) (string, error) {
	return "", nil
}

func (o *operatorBalanceInvoker) EmergencyWithdrawAll(context.Context, string) error { return nil }

func newVaultForOperatorTest(t *testing.T, svc *VaultService, userID uuid.UUID) vault.Vault {
	t.Helper()
	created, err := svc.CreateVault(context.Background(), CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Currency:        "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault: %v", err)
	}
	return created
}

// The acceptance criterion of nester#1152: a deposit must not reduce the
// operator balance.
//
// Before this change DepositToVault was called with the operator as both
// caller and depositing user, so POST /vaults/{id}/deposit spent platform
// funds for anyone who asked.
func TestRecordDeposit_DoesNotDebitOperatorAccount(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemoryVaultRepository(userID)
	svc := NewVaultService(repo)

	invoker := newOperatorBalanceInvoker("1000")
	svc.SetDepositInvoker(invoker)
	created := newVaultForOperatorTest(t, svc, userID)

	before := invoker.operatorBalance

	_, err := svc.RecordDeposit(ctx, RecordDepositInput{
		VaultID: created.ID,
		Amount:  decimal.RequireFromString("100"),
	})
	if !errors.Is(err, vault.ErrOperatorFundedDepositRefused) {
		t.Fatalf("err = %v, want ErrOperatorFundedDepositRefused", err)
	}

	if invoker.depositCalls != 0 {
		t.Errorf("DepositToVault called %d times, want 0", invoker.depositCalls)
	}
	if !invoker.operatorBalance.Equal(before) {
		t.Fatalf("operator balance = %s, want %s (a deposit must not spend platform funds)",
			invoker.operatorBalance, before)
	}
}

// The wallet-signed path is what deposits are supposed to use, and it must
// keep working: the user signs, the chain verifier confirms the event, and the
// operator account is never touched.
func TestRecordDeposit_WalletSignedPathDoesNotTouchOperator(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemoryVaultRepository(userID)
	svc := NewVaultService(repo)

	invoker := newOperatorBalanceInvoker("1000")
	svc.SetDepositInvoker(invoker)
	created := newVaultForOperatorTest(t, svc, userID)

	svc.SetChainEventVerifier(&fakeChainVerifier{events: map[string]VerifiedVaultEvent{
		"user-signed-hash": {
			Amount:     decimal.RequireFromString("100"),
			EventType:  "deposit",
			ContractID: created.ContractAddress,
		},
	}})

	before := invoker.operatorBalance

	updated, err := svc.RecordDeposit(ctx, RecordDepositInput{
		VaultID: created.ID,
		Amount:  decimal.RequireFromString("100"),
		TxHash:  "user-signed-hash",
	})
	if err != nil {
		t.Fatalf("RecordDeposit: %v", err)
	}

	if invoker.depositCalls != 0 {
		t.Errorf("DepositToVault called %d times, want 0 for a wallet-signed deposit", invoker.depositCalls)
	}
	if !invoker.operatorBalance.Equal(before) {
		t.Fatalf("operator balance = %s, want %s", invoker.operatorBalance, before)
	}
	if !updated.CurrentBalance.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("vault balance = %s, want 100", updated.CurrentBalance)
	}
}

// An explicitly allowlisted, capped, operator-funded deposit is still
// permitted: the escape hatch the issue allows for.
func TestRecordDeposit_AllowlistedOperatorFundedDepositIsPermitted(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemoryVaultRepository(userID)
	svc := NewVaultService(repo)

	invoker := newOperatorBalanceInvoker("1000")
	svc.SetDepositInvoker(invoker)
	created := newVaultForOperatorTest(t, svc, userID)

	svc.SetOperatorFundedDepositPolicy(NewOperatorFundedDepositPolicy(
		true,
		[]uuid.UUID{created.ID},
		decimal.RequireFromString("500"),
		discardLogger(),
	))

	if _, err := svc.RecordDeposit(ctx, RecordDepositInput{
		VaultID: created.ID,
		Amount:  decimal.RequireFromString("100"),
	}); err != nil {
		t.Fatalf("RecordDeposit: %v", err)
	}

	if invoker.depositCalls != 1 {
		t.Fatalf("DepositToVault called %d times, want 1", invoker.depositCalls)
	}
	// This path DOES spend operator funds; that is what the allowlist opts
	// into, and why it is capped and logged.
	if !invoker.operatorBalance.Equal(decimal.RequireFromString("900")) {
		t.Fatalf("operator balance = %s, want 900", invoker.operatorBalance)
	}
}

func TestOperatorFundedDepositPolicy_Authorize(t *testing.T) {
	allowed := uuid.New()
	other := uuid.New()
	user := uuid.New()

	tests := []struct {
		name    string
		policy  *OperatorFundedDepositPolicy
		vaultID uuid.UUID
		amount  string
		wantErr error
	}{
		{
			name:    "nil policy refuses",
			policy:  nil,
			vaultID: allowed,
			amount:  "1",
			wantErr: ErrOperatorFundedDepositDisabled,
		},
		{
			name:    "disabled refuses",
			policy:  NewOperatorFundedDepositPolicy(false, []uuid.UUID{allowed}, decimal.NewFromInt(100), discardLogger()),
			vaultID: allowed,
			amount:  "1",
			wantErr: ErrOperatorFundedDepositDisabled,
		},
		{
			name:    "enabled with empty allowlist refuses",
			policy:  NewOperatorFundedDepositPolicy(true, nil, decimal.NewFromInt(100), discardLogger()),
			vaultID: allowed,
			amount:  "1",
			wantErr: ErrOperatorFundedDepositNotAllowed,
		},
		{
			name:    "vault outside the allowlist refuses",
			policy:  NewOperatorFundedDepositPolicy(true, []uuid.UUID{allowed}, decimal.NewFromInt(100), discardLogger()),
			vaultID: other,
			amount:  "1",
			wantErr: ErrOperatorFundedDepositNotAllowed,
		},
		{
			name:    "amount over the cap refuses",
			policy:  NewOperatorFundedDepositPolicy(true, []uuid.UUID{allowed}, decimal.NewFromInt(100), discardLogger()),
			vaultID: allowed,
			amount:  "100.01",
			wantErr: ErrOperatorFundedDepositCapExceeded,
		},
		{
			name:    "zero cap refuses even an allowlisted vault",
			policy:  NewOperatorFundedDepositPolicy(true, []uuid.UUID{allowed}, decimal.Zero, discardLogger()),
			vaultID: allowed,
			amount:  "1",
			wantErr: ErrOperatorFundedDepositCapExceeded,
		},
		{
			name:    "allowlisted and at the cap is permitted",
			policy:  NewOperatorFundedDepositPolicy(true, []uuid.UUID{allowed}, decimal.NewFromInt(100), discardLogger()),
			vaultID: allowed,
			amount:  "100",
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Authorize(context.Background(), tt.vaultID, user, decimal.RequireFromString(tt.amount))
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Authorize() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Authorize() = %v, want %v", err, tt.wantErr)
			}
			// Every refusal must also be recognisable to the HTTP layer.
			if !errors.Is(err, vault.ErrOperatorFundedDepositRefused) {
				t.Fatalf("Authorize() = %v, want it to wrap ErrOperatorFundedDepositRefused", err)
			}
		})
	}
}

func TestParseOperatorFundedVaultIDs(t *testing.T) {
	id1, id2 := uuid.New(), uuid.New()

	got, err := ParseOperatorFundedVaultIDs("  " + id1.String() + " , " + id2.String() + " ")
	if err != nil {
		t.Fatalf("ParseOperatorFundedVaultIDs: %v", err)
	}
	if len(got) != 2 || got[0] != id1 || got[1] != id2 {
		t.Fatalf("got %v, want [%s %s]", got, id1, id2)
	}

	if got, err := ParseOperatorFundedVaultIDs(""); err != nil || got != nil {
		t.Fatalf("empty input: got %v, %v; want nil, nil", got, err)
	}

	// A malformed entry must fail loudly rather than silently shrink the
	// allowlist into something nobody configured.
	if _, err := ParseOperatorFundedVaultIDs(id1.String() + ",not-a-uuid"); err == nil {
		t.Fatal("expected an error for a malformed vault id")
	}
}
