package harvest

import (
	"context"

	"github.com/shopspring/decimal"
)

// StaticGasOracle is a GasOracle backed by a fixed per-harvest fee estimate,
// expressed in settlement-currency units so it is directly comparable to accrued
// yield. Stellar/Soroban base fees are small and stable, so a conservative flat
// estimate is a sound default; a fee-market-aware implementation of GasOracle
// can be substituted without touching the engine.
type StaticGasOracle struct {
	fee       decimal.Decimal
	congested bool
}

// NewStaticGasOracle constructs a StaticGasOracle with the given flat fee.
func NewStaticGasOracle(fee decimal.Decimal) *StaticGasOracle {
	return &StaticGasOracle{fee: fee}
}

// HarvestFee returns the configured flat fee regardless of currency.
func (o *StaticGasOracle) HarvestFee(context.Context, string) (decimal.Decimal, error) {
	return o.fee, nil
}

// Congested reports the configured congestion flag (default false).
func (o *StaticGasOracle) Congested(context.Context) (bool, error) {
	return o.congested, nil
}

// SetCongested toggles the congestion signal. Exposed so an external fee-market
// monitor can drive deferral without replacing the oracle.
func (o *StaticGasOracle) SetCongested(v bool) { o.congested = v }
