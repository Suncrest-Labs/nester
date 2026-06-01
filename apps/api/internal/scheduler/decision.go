// Package scheduler implements the automated vault rebalancing scheduler
// described in nester#372. The package is intentionally split:
//
//   - decision.go (this file) is pure: it computes the optimal allocation
//     from a snapshot of protocol APYs and decides whether the expected
//     APY gain exceeds the configured threshold. It has no side effects
//     and no dependencies on Postgres, Stellar, or HTTP — which is what
//     issue #372's acceptance criterion ("Unit tests for the rebalancing
//     decision logic — no contract calls needed, mock the yield fetcher")
//     calls for.
//   - scheduler.go wires the loop, repository, and chain invoker around
//     this pure core.
package scheduler

import (
	"sort"

	"github.com/shopspring/decimal"
)

// ProtocolYield is a single protocol's reported APY snapshot at a point in
// time. The scheduler does not care where it came from (on-chain query,
// DefiLlama, etc.) — only that APY is comparable across protocols and that
// the protocol name uniquely identifies the destination.
type ProtocolYield struct {
	Protocol string
	APY      decimal.Decimal
}

// CurrentAllocation is the current `vault_allocations` row for a single
// protocol. The full vault state is `[]CurrentAllocation`.
type CurrentAllocation struct {
	Protocol string
	Amount   decimal.Decimal
}

// DecisionInput is the snapshot the decision function takes.
type DecisionInput struct {
	// CurrentAllocations is what the vault holds right now.
	CurrentAllocations []CurrentAllocation
	// Yields is the latest APY per candidate protocol.
	Yields []ProtocolYield
	// MinAPYGainBPS is the minimum expected APY improvement, in basis
	// points, before a rebalance is worth triggering. 50 BPS = 0.5%.
	MinAPYGainBPS int64
}

// Decision is the result of evaluating an input.
type Decision struct {
	// Rebalance is true when ExpectedGainBPS > MinAPYGainBPS AND the
	// optimal protocol differs from the current top allocation.
	Rebalance bool
	// CurrentTopProtocol is the protocol holding the largest current
	// allocation. Used as the baseline when computing gain.
	CurrentTopProtocol string
	// OptimalProtocol is the protocol with the highest reported APY.
	OptimalProtocol string
	// CurrentWeightedAPY is the APY-weighted average of the current
	// allocation, used as the "before" side of the gain.
	CurrentWeightedAPY decimal.Decimal
	// OptimalAPY is the APY we would expect after moving the whole
	// vault into OptimalProtocol.
	OptimalAPY decimal.Decimal
	// ExpectedGainBPS is (OptimalAPY - CurrentWeightedAPY) * 10000.
	// Negative when the current allocation is already at or above the
	// best yield.
	ExpectedGainBPS int64
	// Reason is a short machine-readable code: "below_threshold",
	// "already_optimal", "no_yields", "no_current_allocation",
	// "rebalance".
	Reason string
}

// Decide is pure: same input → same output, no side effects.
//
// Rules:
//   - If `Yields` is empty, no decision can be made → `no_yields`.
//   - If `CurrentAllocations` is empty, treat current APY as 0 — every
//     positive yield clears the threshold, so we recommend rebalancing
//     into the best protocol → `no_current_allocation`.
//   - If the optimal protocol already matches the current top allocation,
//     no rebalance → `already_optimal`. (We don't churn just because a
//     downstream allocation could be re-weighted; that's deferred until
//     #372 has a multi-protocol-weight strategy.)
//   - If `OptimalAPY - CurrentWeightedAPY` in BPS is below MinAPYGainBPS,
//     no rebalance → `below_threshold`.
//   - Otherwise, rebalance → `rebalance`.
func Decide(in DecisionInput) Decision {
	if len(in.Yields) == 0 {
		return Decision{Reason: "no_yields"}
	}

	yields := append([]ProtocolYield(nil), in.Yields...)
	sort.SliceStable(yields, func(i, j int) bool {
		return yields[i].APY.GreaterThan(yields[j].APY)
	})
	optimal := yields[0]

	if len(in.CurrentAllocations) == 0 {
		gainBPS := optimal.APY.Mul(decimal.NewFromInt(10000)).IntPart()
		return Decision{
			Rebalance:          gainBPS >= in.MinAPYGainBPS,
			CurrentTopProtocol: "",
			OptimalProtocol:    optimal.Protocol,
			CurrentWeightedAPY: decimal.Zero,
			OptimalAPY:         optimal.APY,
			ExpectedGainBPS:    gainBPS,
			Reason:             "no_current_allocation",
		}
	}

	yieldByProtocol := make(map[string]decimal.Decimal, len(yields))
	for _, y := range yields {
		yieldByProtocol[y.Protocol] = y.APY
	}

	totalAmount := decimal.Zero
	for _, a := range in.CurrentAllocations {
		totalAmount = totalAmount.Add(a.Amount)
	}
	if totalAmount.IsZero() {
		// Same semantics as "no current allocation" — we can't weight
		// by zero, so treat the baseline APY as zero.
		gainBPS := optimal.APY.Mul(decimal.NewFromInt(10000)).IntPart()
		return Decision{
			Rebalance:          gainBPS >= in.MinAPYGainBPS,
			OptimalProtocol:    optimal.Protocol,
			CurrentWeightedAPY: decimal.Zero,
			OptimalAPY:         optimal.APY,
			ExpectedGainBPS:    gainBPS,
			Reason:             "no_current_allocation",
		}
	}

	weightedAPY := decimal.Zero
	var topProtocol string
	topAmount := decimal.Zero
	for _, a := range in.CurrentAllocations {
		if y, ok := yieldByProtocol[a.Protocol]; ok {
			weightedAPY = weightedAPY.Add(a.Amount.Mul(y))
		}
		if a.Amount.GreaterThan(topAmount) {
			topAmount = a.Amount
			topProtocol = a.Protocol
		}
	}
	weightedAPY = weightedAPY.Div(totalAmount)

	gainBPS := optimal.APY.Sub(weightedAPY).Mul(decimal.NewFromInt(10000)).IntPart()

	if optimal.Protocol == topProtocol {
		return Decision{
			Rebalance:          false,
			CurrentTopProtocol: topProtocol,
			OptimalProtocol:    optimal.Protocol,
			CurrentWeightedAPY: weightedAPY,
			OptimalAPY:         optimal.APY,
			ExpectedGainBPS:    gainBPS,
			Reason:             "already_optimal",
		}
	}

	if gainBPS < in.MinAPYGainBPS {
		return Decision{
			Rebalance:          false,
			CurrentTopProtocol: topProtocol,
			OptimalProtocol:    optimal.Protocol,
			CurrentWeightedAPY: weightedAPY,
			OptimalAPY:         optimal.APY,
			ExpectedGainBPS:    gainBPS,
			Reason:             "below_threshold",
		}
	}

	return Decision{
		Rebalance:          true,
		CurrentTopProtocol: topProtocol,
		OptimalProtocol:    optimal.Protocol,
		CurrentWeightedAPY: weightedAPY,
		OptimalAPY:         optimal.APY,
		ExpectedGainBPS:    gainBPS,
		Reason:             "rebalance",
	}
}
