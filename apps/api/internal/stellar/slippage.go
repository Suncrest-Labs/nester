package stellar

import (
	"errors"
	"fmt"
)

const (
	DefaultWithdrawalSlippageBps = 50
	MaxWithdrawalSlippageBps     = 300
)

var ErrInvalidSlippageBps = errors.New("slippage bps must be between 1 and 300")

// ValidateSlippageBps rejects zero and values above the safe maximum.
func ValidateSlippageBps(bps int) error {
	if bps <= 0 || bps > MaxWithdrawalSlippageBps {
		return ErrInvalidSlippageBps
	}
	return nil
}

// ResolveSlippageBps returns the caller-provided bps when set, otherwise the
// configured default (falling back to DefaultWithdrawalSlippageBps).
func ResolveSlippageBps(requested, configuredDefault int) (int, error) {
	if requested != 0 {
		if err := ValidateSlippageBps(requested); err != nil {
			return 0, err
		}
		return requested, nil
	}

	defaultBps := configuredDefault
	if defaultBps <= 0 {
		defaultBps = DefaultWithdrawalSlippageBps
	}
	if err := ValidateSlippageBps(defaultBps); err != nil {
		return 0, fmt.Errorf("invalid configured default slippage: %w", err)
	}
	return defaultBps, nil
}

// ComputeMinAssetsOut derives the minimum assets-out floor from a preview
// amount and slippage tolerance in basis points.
func ComputeMinAssetsOut(previewAmount int64, slippageBps int) int64 {
	if previewAmount <= 0 {
		return 0
	}
	return previewAmount * int64(10_000-slippageBps) / 10_000
}
