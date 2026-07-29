// Package valuation implements the real-time portfolio valuation service
// (#832): a pure aggregator that values a user's positions, goals, pending
// deposits, and claimable rewards to the stroop, an oracle abstraction for
// multi-asset pricing with confidence propagation, a per-user cache with
// event-driven invalidation, and a WebSocket push hook.
package valuation

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/portfolio"
)

// AssetAmount is an amount denominated in a specific asset.
type AssetAmount struct {
	Asset  string
	Amount decimal.Decimal
}

// Position is a single vault position in asset units. Current value is
// Principal + Yield; Locked is the portion of that value not currently
// withdrawable, and Flexible is the remainder.
type Position struct {
	VaultID   uuid.UUID
	Asset     string
	Principal decimal.Decimal
	Yield     decimal.Decimal
	Locked    decimal.Decimal
}

// GoalInput is a savings goal's allocated and target amounts in asset units.
type GoalInput struct {
	GoalID    uuid.UUID
	Name      string
	Asset     string
	Allocated decimal.Decimal
	Target    decimal.Decimal
}

// Inputs is the fully-materialized set of facts to value. The service gathers
// these from repositories before calling Aggregate, keeping aggregation pure.
type Inputs struct {
	UserID           uuid.UUID
	Now              time.Time
	Positions        []Position
	PendingDeposits  []AssetAmount
	ClaimableRewards []AssetAmount
	Goals            []GoalInput
}

// Price is an asset's USDC exchange rate and the confidence in it.
type Price struct {
	Rate       decimal.Decimal
	Confidence portfolio.Confidence
}

// PriceTable maps asset code to Price.
type PriceTable map[string]Price

// round7 rounds to the USDC stroop scale.
func round7(d decimal.Decimal) decimal.Decimal { return d.Round(portfolio.USDCScale) }

// Aggregate values Inputs against prices and returns a stroop-exact Valuation.
// It errors if any referenced asset has no price — a valuation is never emitted
// from incomplete pricing data. Overall confidence is the worst confidence among
// all contributing prices.
func Aggregate(in Inputs, prices PriceTable) (portfolio.Valuation, error) {
	val := portfolio.Valuation{
		UserID:               in.UserID,
		GeneratedAt:          in.Now.UTC(),
		TotalValueUSDC:       decimal.Zero,
		PrincipalUSDC:        decimal.Zero,
		YieldUSDC:            decimal.Zero,
		FlexibleUSDC:         decimal.Zero,
		LockedUSDC:           decimal.Zero,
		PendingDepositsUSDC:  decimal.Zero,
		ClaimableRewardsUSDC: decimal.Zero,
		Confidence:           portfolio.ConfidenceHigh,
		Vaults:               make([]portfolio.VaultValuation, 0, len(in.Positions)),
		Goals:                make([]portfolio.GoalValuation, 0, len(in.Goals)),
	}
	// contributed tracks whether any priced value was seen, so an empty portfolio
	// keeps the default "high" confidence rather than inheriting a stale asset's.
	worst := portfolio.ConfidenceHigh
	contributed := false
	track := func(c portfolio.Confidence) {
		worst = portfolio.WorseConfidence(worst, c)
		contributed = true
	}

	for _, p := range in.Positions {
		price, ok := prices[p.Asset]
		if !ok {
			return portfolio.Valuation{}, fmt.Errorf("valuation: no price for asset %q (vault %s)", p.Asset, p.VaultID)
		}
		principalUSDC := round7(p.Principal.Mul(price.Rate))
		yieldUSDC := round7(p.Yield.Mul(price.Rate))
		current := principalUSDC.Add(yieldUSDC)
		lockedUSDC := round7(p.Locked.Mul(price.Rate))
		if lockedUSDC.GreaterThan(current) {
			lockedUSDC = current
		}
		flexibleUSDC := current.Sub(lockedUSDC)

		val.Vaults = append(val.Vaults, portfolio.VaultValuation{
			VaultID:          p.VaultID,
			Asset:            p.Asset,
			PrincipalUSDC:    principalUSDC,
			YieldUSDC:        yieldUSDC,
			FlexibleUSDC:     flexibleUSDC,
			LockedUSDC:       lockedUSDC,
			CurrentValueUSDC: current,
			Confidence:       price.Confidence,
		})
		val.PrincipalUSDC = val.PrincipalUSDC.Add(principalUSDC)
		val.YieldUSDC = val.YieldUSDC.Add(yieldUSDC)
		val.FlexibleUSDC = val.FlexibleUSDC.Add(flexibleUSDC)
		val.LockedUSDC = val.LockedUSDC.Add(lockedUSDC)
		val.TotalValueUSDC = val.TotalValueUSDC.Add(current)
		track(price.Confidence)
	}

	pending, err := sumAssets(in.PendingDeposits, prices, track)
	if err != nil {
		return portfolio.Valuation{}, err
	}
	val.PendingDepositsUSDC = pending

	claimable, err := sumAssets(in.ClaimableRewards, prices, track)
	if err != nil {
		return portfolio.Valuation{}, err
	}
	val.ClaimableRewardsUSDC = claimable

	for _, g := range in.Goals {
		price, ok := prices[g.Asset]
		if !ok {
			return portfolio.Valuation{}, fmt.Errorf("valuation: no price for goal asset %q (goal %s)", g.Asset, g.GoalID)
		}
		allocated := round7(g.Allocated.Mul(price.Rate))
		target := round7(g.Target.Mul(price.Rate))
		val.Goals = append(val.Goals, portfolio.GoalValuation{
			GoalID:        g.GoalID,
			Name:          g.Name,
			AllocatedUSDC: allocated,
			TargetUSDC:    target,
			ProgressBps:   progressBps(allocated, target),
		})
		track(price.Confidence)
	}

	// All counted value is settled; pending deposits are excluded from the total.
	val.SettledUSDC = val.TotalValueUSDC
	if contributed {
		val.Confidence = worst
	}
	return val, nil
}

func sumAssets(items []AssetAmount, prices PriceTable, track func(portfolio.Confidence)) (decimal.Decimal, error) {
	total := decimal.Zero
	for _, a := range items {
		price, ok := prices[a.Asset]
		if !ok {
			return decimal.Zero, fmt.Errorf("valuation: no price for asset %q", a.Asset)
		}
		total = total.Add(round7(a.Amount.Mul(price.Rate)))
		track(price.Confidence)
	}
	return total, nil
}

// progressBps returns allocated/target in basis points, clamped to [0, 10000].
func progressBps(allocated, target decimal.Decimal) int {
	if target.LessThanOrEqual(decimal.Zero) {
		return 0
	}
	bps := allocated.Div(target).Mul(decimal.NewFromInt(10000)).IntPart()
	if bps < 0 {
		return 0
	}
	if bps > 10000 {
		return 10000
	}
	return int(bps)
}

// Assets returns the distinct set of asset codes referenced by Inputs, so the
// service can fetch exactly the prices it needs in one oracle call.
func (in Inputs) Assets() []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(a string) {
		if a == "" {
			return
		}
		if _, ok := seen[a]; !ok {
			seen[a] = struct{}{}
			out = append(out, a)
		}
	}
	for _, p := range in.Positions {
		add(p.Asset)
	}
	for _, a := range in.PendingDeposits {
		add(a.Asset)
	}
	for _, a := range in.ClaimableRewards {
		add(a.Asset)
	}
	for _, g := range in.Goals {
		add(g.Asset)
	}
	return out
}
