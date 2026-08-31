package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// shareUnitInvoker records the share amount handed to the contract and models
// a vault whose shares are worth sharePrice assets each.
type shareUnitInvoker struct {
	sharePrice     decimal.Decimal
	gotShares      int64
	previewEnabled bool
}

func (s *shareUnitInvoker) DepositToVault(context.Context, string, int64) error { return nil }

func (s *shareUnitInvoker) WithdrawFromVault(_ context.Context, _ string, sharesStroops int64, _ int) (string, error) {
	s.gotShares = sharesStroops
	return "on-chain-hash", nil
}

func (s *shareUnitInvoker) PreviewDeposit(context.Context, string, int64) (int64, error) {
	return 0, nil
}

// PreviewWithdraw maps shares to the assets they redeem for, which is what the
// real contract's preview does.
func (s *shareUnitInvoker) PreviewWithdraw(_ context.Context, _ string, sharesStroops int64) (int64, error) {
	if !s.previewEnabled {
		return 0, nil
	}
	return decimal.NewFromInt(sharesStroops).Mul(s.sharePrice).Round(0).IntPart(), nil
}

func (s *shareUnitInvoker) HarvestVault(context.Context, string, string, bool) (string, error) {
	return "", nil
}

func (s *shareUnitInvoker) EmergencyWithdrawAll(context.Context, string) error { return nil }

// The contract's withdraw is share-denominated. Passing asset stroops made the
// user burn the wrong number of shares whenever the share price was not
// exactly 1.0 — taking materially more or less than they asked for, while the
// database recorded the requested amount either way (nester#1151).
func TestRecordWithdrawal_ConvertsAssetsToSharesAtSharePrice(t *testing.T) {
	const stroops = 10_000_000

	tests := []struct {
		name string
		// deposited/balance set the share price: price = balance / deposited.
		deposited      string
		balance        string
		withdraw       string
		wantSharePrice string
		wantShares     string
		previewEnabled bool
	}{
		{
			// Price 1.25 (vault gained). 100 assets must burn 80 shares.
			// The old code burned 100 shares, taking 125 assets.
			name:           "share price above 1.0",
			deposited:      "100",
			balance:        "125",
			withdraw:       "100",
			wantSharePrice: "1.25",
			wantShares:     "80",
		},
		{
			// Price 0.8 (vault lost). 40 assets must burn 50 shares.
			// The old code burned 40 shares, taking only 32 assets.
			name:           "share price below 1.0",
			deposited:      "100",
			balance:        "80",
			withdraw:       "40",
			wantSharePrice: "0.8",
			wantShares:     "50",
		},
		{
			// The unity case must keep working unchanged.
			name:           "share price exactly 1.0",
			deposited:      "100",
			balance:        "100",
			withdraw:       "25",
			wantSharePrice: "1",
			wantShares:     "25",
		},
		{
			// With the contract preview wired up the answer must still be the
			// same, since the preview here agrees with the local price.
			name:           "share price above 1.0 with contract preview",
			deposited:      "100",
			balance:        "125",
			withdraw:       "100",
			wantSharePrice: "1.25",
			wantShares:     "80",
			previewEnabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			userID := uuid.New()
			repo := newMemoryVaultRepository(userID)
			svc := NewVaultService(repo)

			created, err := svc.CreateVault(ctx, CreateVaultInput{
				UserID:          userID,
				ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
				Currency:        "USDC",
			})
			if err != nil {
				t.Fatalf("CreateVault: %v", err)
			}

			// Deposit first, then move the balance to set the share price.
			if _, err := svc.RecordDeposit(ctx, RecordDepositInput{
				VaultID: created.ID,
				Amount:  decimal.RequireFromString(tt.deposited),
			}); err != nil {
				t.Fatalf("RecordDeposit: %v", err)
			}
			repo.setBalance(created.ID, decimal.RequireFromString(tt.balance))

			before, err := svc.GetVault(ctx, created.ID)
			if err != nil {
				t.Fatalf("GetVault: %v", err)
			}
			if got := vault.ComputeSharePrice(before); got.String() != tt.wantSharePrice {
				t.Fatalf("precondition: share price = %s, want %s", got, tt.wantSharePrice)
			}

			invoker := &shareUnitInvoker{
				sharePrice:     decimal.RequireFromString(tt.wantSharePrice),
				previewEnabled: tt.previewEnabled,
			}
			svc.SetDepositInvoker(invoker)

			if _, err := svc.RecordWithdrawal(ctx, RecordWithdrawalInput{
				VaultID: created.ID,
				Amount:  decimal.RequireFromString(tt.withdraw),
			}); err != nil {
				t.Fatalf("RecordWithdrawal: %v", err)
			}

			wantShares := decimal.RequireFromString(tt.wantShares).Mul(decimal.NewFromInt(stroops)).IntPart()
			if invoker.gotShares != wantShares {
				t.Errorf("shares burned = %d, want %d (%s shares at price %s for %s assets)",
					invoker.gotShares, wantShares, tt.wantShares, tt.wantSharePrice, tt.withdraw)
			}

			// The asset amount the burn redeems for must match the request.
			gotAssets := decimal.NewFromInt(invoker.gotShares).
				Div(decimal.NewFromInt(stroops)).
				Mul(decimal.RequireFromString(tt.wantSharePrice))
			if !gotAssets.Equal(decimal.RequireFromString(tt.withdraw)) {
				t.Errorf("assets received = %s, want %s", gotAssets, tt.withdraw)
			}
		})
	}
}

// The recorded withdrawal must reflect the shares the chain actually burned,
// not a figure re-derived from the requested amount (nester#1151).
func TestRecordWithdrawal_RecordsSharesActuallyBurned(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemoryVaultRepository(userID)
	svc := NewVaultService(repo)

	created, err := svc.CreateVault(ctx, CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Currency:        "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault: %v", err)
	}
	if _, err := svc.RecordDeposit(ctx, RecordDepositInput{
		VaultID: created.ID,
		Amount:  decimal.RequireFromString("100"),
	}); err != nil {
		t.Fatalf("RecordDeposit: %v", err)
	}
	repo.setBalance(created.ID, decimal.RequireFromString("125"))

	svc.SetDepositInvoker(&shareUnitInvoker{sharePrice: decimal.RequireFromString("1.25")})

	if _, err := svc.RecordWithdrawal(ctx, RecordWithdrawalInput{
		VaultID: created.ID,
		Amount:  decimal.RequireFromString("100"),
	}); err != nil {
		t.Fatalf("RecordWithdrawal: %v", err)
	}

	// 100 assets at 1.25 burns 80 shares, not 100.
	want := decimal.RequireFromString("80")
	got := repo.lastWithdrawalShares()
	if !got.Equal(want) {
		t.Fatalf("recorded shares burned = %s, want %s", got, want)
	}
}
