package vault

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const positionDecimalPlaces = int32(6)

// UserVaultPosition is the aggregated per-user position in a single vault.
type UserVaultPosition struct {
	VaultID            uuid.UUID  `json:"vault_id"`
	UserID             uuid.UUID  `json:"user_id"`
	TotalDepositedUSDC string     `json:"total_deposited_usdc"`
	SharesHeld         string     `json:"shares_held"`
	CurrentValueUSDC   string     `json:"current_value_usdc"`
	UnrealizedPnLUSDC  string     `json:"unrealized_pnl_usdc"`
	UnrealizedPnLPct   string     `json:"unrealized_pnl_pct"`
	FeesPaidUSDC       string     `json:"fees_paid_usdc"`
	FirstDepositAt     *time.Time `json:"first_deposit_at"`
	LastActivityAt     *time.Time `json:"last_activity_at"`
}

// TransactionRecord captures ledger metadata written alongside a deposit or withdrawal.
type TransactionRecord struct {
	UserID               uuid.UUID
	Amount               decimal.Decimal
	TransactionHash      string
	SharesMintedOrBurned decimal.Decimal
	SharePriceAtTime     decimal.Decimal
	FeeCharged           decimal.Decimal
}

// ComputeSharePrice returns the live NAV per share for a vault.
func ComputeSharePrice(v Vault) decimal.Decimal {
	if v.TotalDeposited.IsZero() || v.TotalDeposited.Sign() <= 0 {
		return decimal.NewFromInt(1)
	}
	return v.CurrentBalance.Div(v.TotalDeposited).Round(8)
}

// BuildUserVaultPosition aggregates user transactions and applies live vault pricing.
func BuildUserVaultPosition(v Vault, userID uuid.UUID, txns []VaultTransaction) UserVaultPosition {
	if len(txns) == 0 {
		return EmptyUserVaultPosition(v.ID, userID)
	}

	totalDeposited := decimal.Zero
	totalWithdrawn := decimal.Zero
	sharesHeld := decimal.Zero
	feesPaid := decimal.Zero

	var firstDepositAt *time.Time
	var lastActivityAt *time.Time

	for _, txn := range txns {
		if lastActivityAt == nil || txn.CreatedAt.After(*lastActivityAt) {
			t := txn.CreatedAt
			lastActivityAt = &t
		}

		shares := decimal.Zero
		if txn.SharesMintedOrBurned != nil {
			shares = *txn.SharesMintedOrBurned
		}

		fee := decimal.Zero
		if txn.FeeCharged != nil {
			fee = *txn.FeeCharged
		}
		feesPaid = feesPaid.Add(fee)

		switch txn.Type {
		case "deposit":
			totalDeposited = totalDeposited.Add(txn.Amount)
			sharesHeld = sharesHeld.Add(shares)
			if firstDepositAt == nil || txn.CreatedAt.Before(*firstDepositAt) {
				t := txn.CreatedAt
				firstDepositAt = &t
			}
		case "withdrawal":
			totalWithdrawn = totalWithdrawn.Add(txn.Amount)
			sharesHeld = sharesHeld.Sub(shares)
		}
	}

	if sharesHeld.Sign() < 0 {
		sharesHeld = decimal.Zero
	}

	sharePrice := ComputeSharePrice(v)
	currentValue := sharesHeld.Mul(sharePrice).Round(positionDecimalPlaces)
	netInvested := totalDeposited.Sub(totalWithdrawn)
	unrealizedPnL := currentValue.Sub(netInvested)

	pnlPct := decimal.Zero
	if netInvested.Sign() > 0 {
		pnlPct = unrealizedPnL.Div(netInvested).Mul(decimal.NewFromInt(100)).Round(2)
	}

	return UserVaultPosition{
		VaultID:            v.ID,
		UserID:             userID,
		TotalDepositedUSDC: formatUSDC(totalDeposited),
		SharesHeld:         formatUSDC(sharesHeld),
		CurrentValueUSDC:   formatUSDC(currentValue),
		UnrealizedPnLUSDC:  formatSignedUSDC(unrealizedPnL),
		UnrealizedPnLPct:   formatSignedPct(pnlPct),
		FeesPaidUSDC:       formatUSDC(feesPaid),
		FirstDepositAt:     firstDepositAt,
		LastActivityAt:     lastActivityAt,
	}
}

// EmptyUserVaultPosition returns a zero-valued position for users with no activity.
func EmptyUserVaultPosition(vaultID, userID uuid.UUID) UserVaultPosition {
	return UserVaultPosition{
		VaultID:            vaultID,
		UserID:             userID,
		TotalDepositedUSDC: "0.000000",
		SharesHeld:         "0.000000",
		CurrentValueUSDC:   "0.000000",
		UnrealizedPnLUSDC:  "+0.000000",
		UnrealizedPnLPct:   "+0.00",
		FeesPaidUSDC:       "0.000000",
	}
}

func formatUSDC(value decimal.Decimal) string {
	return value.Round(positionDecimalPlaces).StringFixed(positionDecimalPlaces)
}

func formatSignedUSDC(value decimal.Decimal) string {
	sign := "+"
	if value.Sign() < 0 {
		sign = "-"
	}
	return sign + value.Abs().Round(positionDecimalPlaces).StringFixed(positionDecimalPlaces)
}

func formatSignedPct(value decimal.Decimal) string {
	sign := "+"
	if value.Sign() < 0 {
		sign = "-"
	}
	return sign + value.Abs().Round(2).StringFixed(2)
}
