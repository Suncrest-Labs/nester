// Package harvest implements the yield-harvest orchestration engine (#845).
//
// The engine periodically (and on demand) evaluates each vault's accrued yield
// against an economic threshold — harvest only when the yield exceeds the gas
// cost plus a safety margin — defers when the network is congested, and submits
// harvests as durable, idempotent jobs on the #824 job queue rather than
// executing them inline. The decision core here is pure and unit-tested; the
// engine and job handler wire it to the queue and chain.
package harvest

import (
	"time"

	"github.com/shopspring/decimal"
)

// Reason explains a gating decision, for logs and user-facing status.
type Reason string

const (
	// ReasonHarvest: yield clears the threshold; harvest now.
	ReasonHarvest Reason = "harvest"
	// ReasonNoYield: no positive accrued yield.
	ReasonNoYield Reason = "no_yield"
	// ReasonBelowThreshold: yield is positive but does not cover gas + margin.
	ReasonBelowThreshold Reason = "below_threshold"
	// ReasonNotDue: the vault's configured harvest frequency has not elapsed
	// since its last harvest (#940).
	ReasonNotDue Reason = "not_due"
)

// FrequencyDuration maps a per-vault harvest frequency (#940) to the minimum
// interval that must elapse between harvests. Unrecognized values fall back
// to the daily cadence.
func FrequencyDuration(frequency string) time.Duration {
	switch frequency {
	case "weekly":
		return 7 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// DueForHarvest reports whether enough time has elapsed since lastHarvestedAt
// for frequency to permit another harvest. A nil lastHarvestedAt (never
// harvested) is always due.
func DueForHarvest(now time.Time, frequency string, lastHarvestedAt *time.Time) bool {
	if lastHarvestedAt == nil {
		return true
	}
	return now.Sub(*lastHarvestedAt) >= FrequencyDuration(frequency)
}

// GatingInput carries the economics of a single harvest decision. All amounts
// are in the vault's settlement currency (e.g. USDC), same scale as the ledger.
type GatingInput struct {
	AccruedYield decimal.Decimal
	GasFee       decimal.Decimal
	Margin       decimal.Decimal
}

// GatingDecision is the outcome of Evaluate.
type GatingDecision struct {
	Harvest bool
	// Threshold is GasFee + Margin — the minimum accrued yield to harvest.
	Threshold decimal.Decimal
	// NetGain is AccruedYield - Threshold (may be negative when not harvesting).
	NetGain decimal.Decimal
	Reason  Reason
}

// Evaluate applies the core economic gate: harvest iff accrued yield strictly
// exceeds gas fee plus margin. Negative gas/margin inputs are floored at zero so
// a bad estimate can never invert the gate.
func Evaluate(in GatingInput) GatingDecision {
	gas := floorZero(in.GasFee)
	margin := floorZero(in.Margin)
	threshold := gas.Add(margin)
	net := in.AccruedYield.Sub(threshold)

	switch {
	case in.AccruedYield.LessThanOrEqual(decimal.Zero):
		return GatingDecision{Harvest: false, Threshold: threshold, NetGain: net, Reason: ReasonNoYield}
	case in.AccruedYield.GreaterThan(threshold):
		return GatingDecision{Harvest: true, Threshold: threshold, NetGain: net, Reason: ReasonHarvest}
	default:
		return GatingDecision{Harvest: false, Threshold: threshold, NetGain: net, Reason: ReasonBelowThreshold}
	}
}

// EstimateNextHarvest projects how long until accrued yield reaches the
// threshold, given a constant accrual rate (in currency units per hour). It
// returns ok=false when the estimate is undefined (non-positive rate). When the
// threshold is already met the duration is zero.
func EstimateNextHarvest(accrued, threshold, ratePerHour decimal.Decimal) (time.Duration, bool) {
	if ratePerHour.LessThanOrEqual(decimal.Zero) {
		return 0, false
	}
	remaining := threshold.Sub(accrued)
	if remaining.LessThanOrEqual(decimal.Zero) {
		return 0, true
	}
	hours := remaining.Div(ratePerHour)
	secs := hours.Mul(decimal.NewFromInt(int64(time.Hour / time.Second)))
	// Guard against absurd/overflowing projections.
	if secs.GreaterThan(decimal.NewFromInt(int64((365 * 24 * time.Hour) / time.Second))) {
		return 0, false
	}
	return time.Duration(secs.IntPart()) * time.Second, true
}

func floorZero(d decimal.Decimal) decimal.Decimal {
	if d.LessThan(decimal.Zero) {
		return decimal.Zero
	}
	return d
}
