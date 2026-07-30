package reconciliation

import "time"

type CadenceConfig struct {
	Balance     time.Duration
	Transaction time.Duration
	Invariant   time.Duration
}

func DefaultCadenceConfig() CadenceConfig {
	return CadenceConfig{
		Balance:     5 * time.Minute,
		Transaction: 2 * time.Minute,
		Invariant:   15 * time.Minute,
	}
}

func (c CadenceConfig) CadenceFor(level Level) time.Duration {
	defaults := DefaultCadenceConfig()
	switch level {
	case LevelBalance:
		if c.Balance > 0 {
			return c.Balance
		}
		return defaults.Balance
	case LevelTransaction:
		if c.Transaction > 0 {
			return c.Transaction
		}
		return defaults.Transaction
	case LevelInvariant:
		if c.Invariant > 0 {
			return c.Invariant
		}
		return defaults.Invariant
	default:
		return 0
	}
}

func CheckpointKey(level Level, comparator string) string {
	return "reconciliation:" + string(level) + ":" + comparator
}
