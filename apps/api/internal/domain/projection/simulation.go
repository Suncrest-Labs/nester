package projection

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// This file adds Monte Carlo savings forecasting (issue #843) on top of the
// existing deterministic projection types in model.go. See README.md in this
// package for the full write-up of every distributional assumption; the
// short version is repeated in the doc comments below.

// ---------------------------------------------------------------------------
// Service-facing request/response types
// ---------------------------------------------------------------------------

// SimulationInput is the request for a Monte Carlo savings projection. It is
// deliberately permissive: callers can either supply every parameter
// directly (generic "what if" tool usage) or point at a VaultID/GoalID and
// let the service resolve APY volatility, contribution cadence, and
// target/deadline from real vault performance history and the user's own
// savings goal + schedule.
type SimulationInput struct {
	VaultID *uuid.UUID `json:"vault_id,omitempty"`
	GoalID  *uuid.UUID `json:"goal_id,omitempty"`

	InitialDeposit      decimal.Decimal   `json:"initial_deposit"`
	MonthlyContribution decimal.Decimal   `json:"monthly_contribution"`
	APY                 *decimal.Decimal  `json:"apy,omitempty"` // optional override/explicit mean APY
	PeriodMonths        int               `json:"period_months"`
	CompoundFrequency   CompoundFrequency `json:"compound_frequency"`

	// TargetAmount/DeadlineMonths enable goal-success-probability. If GoalID
	// is set and these are omitted, they are resolved from the goal.
	TargetAmount   *decimal.Decimal `json:"target_amount,omitempty"`
	DeadlineMonths *int             `json:"deadline_months,omitempty"`

	// PathCount lets a caller request a specific sample count; 0 uses
	// DefaultPathCount. Bounded to [MinPathCount, MaxPathCount].
	PathCount int `json:"path_count,omitempty"`
}

// Validate checks the request is well-formed. APY is only required when no
// VaultID is supplied (a vault-grounded request derives its own mean APY from
// history), and PeriodMonths is required either directly or implicitly (a
// vault/goal request may synthesize it from the deadline).
func (s *SimulationInput) Validate() error {
	if s.InitialDeposit.LessThan(decimal.Zero) {
		return ErrInvalidAmount
	}
	if s.MonthlyContribution.LessThan(decimal.Zero) {
		return errors.New("monthly contribution cannot be negative")
	}
	if s.VaultID == nil && s.GoalID == nil {
		if s.InitialDeposit.LessThanOrEqual(decimal.Zero) {
			return ErrInvalidAmount
		}
		if s.APY == nil || s.APY.LessThanOrEqual(decimal.Zero) {
			return ErrInvalidAPY
		}
		if s.PeriodMonths <= 0 {
			return ErrInvalidPeriod
		}
	}
	if s.APY != nil && s.APY.LessThanOrEqual(decimal.Zero) {
		return ErrInvalidAPY
	}
	if s.PeriodMonths < 0 {
		return ErrInvalidPeriod
	}
	// Reject unreasonably long horizons outright (CodeQL flagged the
	// unbounded PeriodMonths/DeadlineMonths -> make([]T, months) allocations
	// in RunMonteCarloSimulation as a memory-exhaustion vector: a caller
	// supplying e.g. period_months=2_000_000_000 would otherwise reach an
	// allocation of that size before any other check fires). MaxPeriodMonths
	// (50 years) is far beyond any realistic savings horizon.
	if s.PeriodMonths > MaxPeriodMonths {
		return ErrPeriodTooLong
	}
	if s.DeadlineMonths != nil && *s.DeadlineMonths <= 0 {
		return ErrInvalidPeriod
	}
	if s.DeadlineMonths != nil && *s.DeadlineMonths > MaxPeriodMonths {
		return ErrPeriodTooLong
	}
	return nil
}

// PercentileTimelinePoint is one month's balance distribution summary.
type PercentileTimelinePoint struct {
	Month int             `json:"month"`
	P10   decimal.Decimal `json:"p10"`
	P50   decimal.Decimal `json:"p50"`
	P90   decimal.Decimal `json:"p90"`
}

// PercentileBand is a standalone P10/P50/P90 summary (used for the
// final-month band).
type PercentileBand struct {
	P10 decimal.Decimal `json:"p10"`
	P50 decimal.Decimal `json:"p50"`
	P90 decimal.Decimal `json:"p90"`
}

// GoalSuccessProbability is the fraction of simulated paths whose balance
// reached TargetAmount by DeadlineMonths.
type GoalSuccessProbability struct {
	TargetAmount   decimal.Decimal `json:"target_amount"`
	DeadlineMonths int             `json:"deadline_months"`
	Probability    float64         `json:"probability"`
}

// SensitivityGridPoint is one cell of a deposit/deadline "what if" grid: the
// resulting goal-success probability when the base monthly contribution is
// adjusted by MonthlyContributionDelta and the deadline by
// DeadlineMonthsDelta.
type SensitivityGridPoint struct {
	MonthlyContributionDelta decimal.Decimal `json:"monthly_contribution_delta"`
	DeadlineMonthsDelta      int             `json:"deadline_months_delta"`
	SuccessProbability       float64         `json:"success_probability"`
}

// SimulationOutput is the full Monte Carlo result returned to callers.
type SimulationOutput struct {
	VaultID  *uuid.UUID      `json:"vault_id,omitempty"`
	Currency string          `json:"currency"`
	Input    SimulationInput `json:"input"`

	// ExpectedAPY/APYStdDev/ContributionSkipProbability document the
	// resolved distributional parameters actually used, so a caller can see
	// why a band is tight or wide.
	ExpectedAPY                 float64 `json:"expected_apy"`
	APYStdDev                   float64 `json:"apy_std_dev"`
	ContributionSkipProbability float64 `json:"contribution_skip_probability"`
	VolatilitySource            string  `json:"volatility_source"` // "historical" | "default_prior"
	// ContributionSource documents where ContributionSkipProbability (and
	// the paired contribution-size variation) came from: "schedule" when
	// derived from the user's own active savingsschedule.SavingsSchedule,
	// or "default_prior" for the documented new-user prior.
	ContributionSource string `json:"contribution_source"` // "schedule" | "default_prior"

	PathCount int   `json:"path_count"`
	Seed      int64 `json:"seed"`

	Timeline        []PercentileTimelinePoint `json:"timeline"`
	FinalBand       PercentileBand            `json:"final_band"`
	GoalSuccess     *GoalSuccessProbability   `json:"goal_success,omitempty"`
	SensitivityGrid []SensitivityGridPoint    `json:"sensitivity_grid,omitempty"`

	CalculatedAt time.Time `json:"calculated_at"`
}

// ---------------------------------------------------------------------------
// Pure Monte Carlo engine
// ---------------------------------------------------------------------------

// DefaultPathCount is the number of simulated paths used when the caller
// does not specify one. Chosen as the smallest sample size that keeps
// P10/P50/P90 stable to the cent across repeated runs for typical horizons
// (see TestRunMonteCarloSimulation_DeterministicGivenSeed) while completing
// in low single-digit milliseconds, which is what keeps the endpoint usable
// inline rather than needing a background job queue.
const DefaultPathCount = 3000

// MaxPathCount bounds the sample count so a caller can't force an
// unbounded/slow simulation.
const MaxPathCount = 5000

// MinPathCount is the floor path count for a still-meaningful distribution.
const MinPathCount = 500

// MaxPeriodMonths bounds the simulated horizon (50 years) so
// RunMonteCarloSimulation's make([]T, months)-shaped allocations can never
// be driven to an unbounded size by a caller-supplied PeriodMonths or
// DeadlineMonths. SimulationInput.Validate rejects anything longer outright;
// RunMonteCarloSimulation also clamps to this bound directly (defense in
// depth for any caller that doesn't route through Validate first).
const MaxPeriodMonths = 600

// MonteCarloParams is the fully-resolved, side-effect-free input to the
// simulation engine. Everything the engine needs is a plain value - no
// repositories, no context, no clock - so RunMonteCarloSimulation is a pure
// function: identical params (in particular an identical Seed) always
// produce bit-for-bit identical output.
type MonteCarloParams struct {
	InitialDeposit      decimal.Decimal
	MonthlyContribution decimal.Decimal
	ExpectedAPY         decimal.Decimal // decimal fraction, e.g. 0.08 = 8%
	APYStdDev           decimal.Decimal // decimal fraction std dev of annual yield
	PeriodMonths        int
	CompoundFrequency   CompoundFrequency

	// ContributionSkipProbability is the probability, in [0,1], that a
	// given period's scheduled contribution does not happen.
	ContributionSkipProbability float64
	// ContributionVariationPct is the max fractional +/- variation applied
	// to a contribution that does happen (uniform on
	// [1-pct, 1+pct] * MonthlyContribution).
	ContributionVariationPct float64

	// PathCount is clamped to [MinPathCount, MaxPathCount]; 0 uses
	// DefaultPathCount.
	PathCount int
	// Seed drives every random draw. Same Seed + same params => same output.
	Seed int64

	// TargetAmount/TargetMonth, if both set, enable goal-success
	// probability: the fraction of simulated paths whose balance reaches
	// TargetAmount by TargetMonth (inclusive, clamped to PeriodMonths).
	TargetAmount *decimal.Decimal
	TargetMonth  *int
}

// MonteCarloResult is the engine's output: percentile bands per month, the
// final-month band, and (if a target was set) the goal-success probability.
type MonteCarloResult struct {
	Timeline    []PercentileTimelinePoint
	FinalBand   PercentileBand
	GoalSuccess *GoalSuccessProbability
	PathCount   int
	Seed        int64
}

// RunMonteCarloSimulation runs PathCount independent simulated paths over
// PeriodMonths. Each path, each month: draw a random annual yield from
// Normal(ExpectedAPY, APYStdDev), convert to a monthly rate; decide whether
// this month's contribution happens (skipped with probability
// ContributionSkipProbability) and if so vary its size uniformly by +/-
// ContributionVariationPct; add the contribution, then compound. Collect
// every path's ending balance at every month into P10/P50/P90.
//
// It is a pure function of its input - see the package README for the full
// list of distributional assumptions this implements.
func RunMonteCarloSimulation(p MonteCarloParams) MonteCarloResult {
	months := p.PeriodMonths
	if months <= 0 || months > MaxPeriodMonths {
		return MonteCarloResult{PathCount: 0, Seed: p.Seed}
	}

	n := p.PathCount
	switch {
	case n <= 0:
		n = DefaultPathCount
	case n > MaxPathCount:
		n = MaxPathCount
	case n < MinPathCount:
		n = MinPathCount
	}

	rng := rand.New(rand.NewSource(p.Seed))

	meanAnnual := p.ExpectedAPY.InexactFloat64()
	stdAnnual := p.APYStdDev.InexactFloat64()
	if stdAnnual < 0 {
		stdAnnual = 0
	}
	contribution := p.MonthlyContribution.InexactFloat64()
	skipProb := clamp01(p.ContributionSkipProbability)
	variationPct := p.ContributionVariationPct
	if variationPct < 0 {
		variationPct = 0
	}

	var targetF float64
	hasTarget := p.TargetAmount != nil && p.TargetMonth != nil && *p.TargetMonth > 0
	targetMonth := 0
	if hasTarget {
		targetF = p.TargetAmount.InexactFloat64()
		targetMonth = *p.TargetMonth
		if targetMonth > months {
			targetMonth = months
		}
	}

	// balances[m] holds every path's ending balance at month m+1.
	balances := make([][]float64, months)
	for m := range balances {
		balances[m] = make([]float64, n)
	}

	successCount := 0
	initial := p.InitialDeposit.InexactFloat64()

	for path := 0; path < n; path++ {
		balance := initial
		reached := false
		for m := 1; m <= months; m++ {
			// Yield draw: annual yield ~ Normal(meanAnnual, stdAnnual),
			// floored at -100% (can't lose more than the balance itself in
			// a single period in this model).
			annualYield := meanAnnual + rng.NormFloat64()*stdAnnual
			if annualYield < -1 {
				annualYield = -1
			}
			monthlyRate := monthlyRateFromAnnual(annualYield, p.CompoundFrequency)

			// Contribution behavior: the skip-check (and, when not
			// skipped, the variation draw) is made every month regardless
			// of the contribution's magnitude, so the random stream
			// consumed is identical no matter what MonthlyContribution is.
			// That invariant is what makes SensitivityGrid's
			// deposit-monotonicity guarantee exact rather than
			// probabilistic (see SensitivityGrid doc comment).
			if m > 1 {
				if rng.Float64() >= skipProb {
					variation := 1 + (rng.Float64()*2-1)*variationPct
					balance += contribution * variation
				}
			}

			balance += balance * monthlyRate
			balances[m-1][path] = balance

			if hasTarget && m <= targetMonth && balance >= targetF {
				reached = true
			}
		}
		if reached {
			successCount++
		}
	}

	timeline := make([]PercentileTimelinePoint, months)
	sorted := make([]float64, n)
	for m := 0; m < months; m++ {
		copy(sorted, balances[m])
		sort.Float64s(sorted)
		timeline[m] = PercentileTimelinePoint{
			Month: m + 1,
			P10:   decimal.NewFromFloat(percentile(sorted, 10)).Round(2),
			P50:   decimal.NewFromFloat(percentile(sorted, 50)).Round(2),
			P90:   decimal.NewFromFloat(percentile(sorted, 90)).Round(2),
		}
	}

	result := MonteCarloResult{Timeline: timeline, PathCount: n, Seed: p.Seed}
	if months > 0 {
		last := timeline[months-1]
		result.FinalBand = PercentileBand{P10: last.P10, P50: last.P50, P90: last.P90}
	}
	if hasTarget {
		result.GoalSuccess = &GoalSuccessProbability{
			TargetAmount:   *p.TargetAmount,
			DeadlineMonths: targetMonth,
			Probability:    float64(successCount) / float64(n),
		}
	}
	return result
}

// monthlyRateFromAnnual mirrors CompoundInterestCalculator's annual-to-
// monthly-rate conversion (service/projection_service.go) so a
// zero-volatility Monte Carlo run collapses to the same numbers as the
// deterministic calculator.
func monthlyRateFromAnnual(annualYield float64, freq CompoundFrequency) float64 {
	if freq == CompoundDaily {
		dailyRate := annualYield / 365
		return math.Pow(1+dailyRate, 365.0/12.0) - 1
	}
	return annualYield / 12
}

// percentile returns the linearly-interpolated pct-th percentile (0-100) of
// an already-sorted slice.
func percentile(sorted []float64, pct float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := (pct / 100) * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	frac := rank - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// MeanStdDev returns the arithmetic mean and population standard deviation
// of values. Pure helper used to translate a vault's raw historical APY
// samples (percentages) into the yield-volatility parameters consumed by
// RunMonteCarloSimulation.
func MeanStdDev(values []float64) (mean, stddev float64) {
	if len(values) == 0 {
		return 0, 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean = sum / float64(len(values))
	var ss float64
	for _, v := range values {
		d := v - mean
		ss += d * d
	}
	stddev = math.Sqrt(ss / float64(len(values)))
	return mean, stddev
}

// SensitivityGrid re-runs the simulation across a small grid of
// monthly-contribution and deadline deltas relative to base, using the SAME
// Seed for every cell (a "common random numbers" variance-reduction
// technique). Reusing the RNG stream means every path draws identical
// yield/skip/variation outcomes across grid cells, so increasing the
// contribution can only ever add to a path's balance at every month
// (monthlyRateFromAnnual never returns a multiplier <= -1, so a larger
// running balance stays larger after compounding). That makes
// SuccessProbability monotonically non-decreasing in contribution for a
// fixed deadline an exact guarantee of this implementation, not a
// probabilistic property of the sampling - see
// TestSensitivityGrid_MoreDepositNeverLowersSuccessProbability.
func SensitivityGrid(base MonteCarloParams, contributionDeltas []decimal.Decimal, deadlineDeltas []int) []SensitivityGridPoint {
	grid := make([]SensitivityGridPoint, 0, len(contributionDeltas)*len(deadlineDeltas))
	for _, cd := range contributionDeltas {
		for _, dd := range deadlineDeltas {
			params := base
			params.MonthlyContribution = base.MonthlyContribution.Add(cd)
			if params.MonthlyContribution.IsNegative() {
				params.MonthlyContribution = decimal.Zero
			}

			if base.TargetMonth != nil {
				deadline := *base.TargetMonth + dd
				if deadline < 1 {
					deadline = 1
				}
				params.TargetMonth = &deadline
				if deadline > params.PeriodMonths {
					params.PeriodMonths = deadline
				}
			}

			res := RunMonteCarloSimulation(params)
			prob := 0.0
			if res.GoalSuccess != nil {
				prob = res.GoalSuccess.Probability
			}
			grid = append(grid, SensitivityGridPoint{
				MonthlyContributionDelta: cd,
				DeadlineMonthsDelta:      dd,
				SuccessProbability:       prob,
			})
		}
	}
	return grid
}

// DeriveSeed produces a stable, deterministic RNG seed from stable request
// inputs (vault/goal id, amounts, deadline, ...). Identical inputs always
// produce the identical seed, and therefore - given
// RunMonteCarloSimulation's determinism - identical output. This is what
// lets results be safely cached and reproduced within a caching window: two
// requests with the same inputs get the same band even if served by
// different API replicas.
func DeriveSeed(parts ...string) int64 {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0}) // separator so ("ab","c") != ("a","bc")
	}
	sum := h.Sum(nil)
	// Mask off the sign bit so the seed is always a non-negative int64;
	// math/rand.NewSource accepts any int64 but a stable non-negative value
	// is friendlier to log/debug output.
	v := binary.BigEndian.Uint64(sum[:8]) &^ (1 << 63)
	return int64(v)
}
