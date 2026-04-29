package validation

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	// Stellar addresses start with 'G' and are 56 characters long.
	stellarAddrRegexp = regexp.MustCompile(`^G[A-Z2-7]{55}$`)
)

const (
	MinDeposit = "10.0"   // Minimum deposit 10 units
	MaxDeposit = "10000.0" // Maximum deposit 10,000 units
)

// WalletAddress validates a Stellar wallet address.
func WalletAddress(addr string) error {
	if !stellarAddrRegexp.MatchString(addr) {
		return errors.New("invalid wallet address format: must be a valid Stellar public key starting with 'G'")
	}
	return nil
}

// VaultID validates that the string is a valid UUID.
func VaultID(id string) (uuid.UUID, error) {
	val, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, errors.New("invalid vault_id: must be a valid UUID")
	}
	return val, nil
}

// DepositAmount validates that the amount is > 0 and within limits.
func DepositAmount(amount string) (decimal.Decimal, error) {
	d, err := decimal.NewFromString(amount)
	if err != nil {
		return decimal.Zero, errors.New("invalid amount: must be a numeric value")
	}

	if d.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, errors.New("amount must be greater than zero")
	}

	min, _ := decimal.NewFromString(MinDeposit)
	max, _ := decimal.NewFromString(MaxDeposit)

	if d.LessThan(min) {
		return decimal.Zero, fmt.Errorf("amount below minimum deposit of %s", MinDeposit)
	}
	if d.GreaterThan(max) {
		return decimal.Zero, fmt.Errorf("amount exceeds maximum deposit of %s", MaxDeposit)
	}

	return d, nil
}

// SettlementDestination performs basic format safety and non-empty checks.
func SettlementDestination(dest string) error {
	trimmed := strings.TrimSpace(dest)
	if trimmed == "" {
		return errors.New("settlement destination cannot be empty")
	}
	if len(trimmed) > 256 {
		return errors.New("settlement destination too long")
	}
	// Basic protection against obvious injection/control characters
	for _, r := range trimmed {
		if r < 32 || r == 127 {
			return errors.New("invalid characters in settlement destination")
		}
	}
	return nil
}
