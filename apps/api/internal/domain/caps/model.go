// Package caps enforces deposit limits: a per-vault soft capacity and a
// per-user rolling 24h cap across all vaults (nester#1316), plus a
// mainnet-only hard TVL cap per vault (nester#1376). Kept dependency-free,
// like domain/moneypath, so both the service layer and the postgres
// repository can depend on it without an import cycle.
package caps

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ErrVaultCapExceeded is returned when crediting a deposit would push a
// vault's current_balance past its soft_capacity.
var ErrVaultCapExceeded = errors.New("deposit would exceed vault capacity limit")

// ErrUserDailyCapExceeded is returned when a deposit would push a user's
// trailing-24h deposit total past their daily_deposit_cap.
var ErrUserDailyCapExceeded = errors.New("deposit would exceed user daily deposit limit")

// ErrTVLCapExceeded is returned when crediting a deposit would push a
// vault's total value locked past the mainnet-only hard TVL cap configured
// for it (nester#1376). Unlike soft_capacity, this cap exists only to limit
// exposure while mainnet does not yet have an operating track record; it is
// never enforced on testnet.
var ErrTVLCapExceeded = errors.New("vault TVL cap exceeded on mainnet")

// CheckVaultCap reports whether depositing amount into a vault currently at
// currentBalance would exceed cap. A nil cap means no limit.
func CheckVaultCap(currentBalance decimal.Decimal, cap *decimal.Decimal, amount decimal.Decimal) error {
	if cap == nil {
		return nil
	}
	if currentBalance.Add(amount).GreaterThan(*cap) {
		return ErrVaultCapExceeded
	}
	return nil
}

// CheckUserDailyCap reports whether depositing amount, on top of a user's
// existing rolling24hTotal, would exceed cap. A nil cap means no limit.
func CheckUserDailyCap(rolling24hTotal decimal.Decimal, cap *decimal.Decimal, amount decimal.Decimal) error {
	if cap == nil {
		return nil
	}
	if rolling24hTotal.Add(amount).GreaterThan(*cap) {
		return ErrUserDailyCapExceeded
	}
	return nil
}

// CheckTVLCap reports whether depositing amount into a vault currently at
// currentTVL would exceed the mainnet hard cap. A nil or non-positive cap
// means no limit (the default, since the cap is configured per-deployment
// and most environments are not mainnet).
func CheckTVLCap(currentTVL decimal.Decimal, cap *decimal.Decimal, amount decimal.Decimal) error {
	if cap == nil || cap.Sign() <= 0 {
		return nil
	}
	if currentTVL.Add(amount).GreaterThan(*cap) {
		return ErrTVLCapExceeded
	}
	return nil
}

// VaultTVLCapManager checks and enforces the mainnet-only hard TVL cap per
// vault (nester#1376). Implementations are expected to read the vault's
// current TVL and the configured ceiling, then delegate to CheckTVLCap.
type VaultTVLCapManager interface {
	CheckDepositCap(ctx context.Context, vaultID uuid.UUID, currentTVL decimal.Decimal, depositAmount decimal.Decimal) error
}
