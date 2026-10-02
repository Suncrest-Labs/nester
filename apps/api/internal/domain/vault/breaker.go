package vault

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

var (
	ErrVaultPausedByBreaker = errors.New("vault is paused by withdrawal circuit breaker pending manual review")
)

type OutflowBreakerConfig struct {
	Enabled          bool
	ThresholdPercent decimal.Decimal // e.g., 20.0 for 20%
	Window           time.Duration   // e.g., 1 * time.Hour
}

// DefaultOutflowBreakerConfig returns the breaker's default policy. Enabled
// defaults to false: the breaker is new behaviour that can halt withdrawals
// on legitimate large or whole-balance redemptions (nester#1377), so it must
// be explicitly turned on by an operator (WITHDRAWAL_BREAKER_ENABLED, or
// VaultService.SetOutflowBreakerConfig) rather than silently changing the
// behaviour of every existing deployment and test the moment this ships.
func DefaultOutflowBreakerConfig() OutflowBreakerConfig {
	return OutflowBreakerConfig{
		Enabled:          false,
		ThresholdPercent: decimal.NewFromInt(25),
		Window:           1 * time.Hour,
	}
}
