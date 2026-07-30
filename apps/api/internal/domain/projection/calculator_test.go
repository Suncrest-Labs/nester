package projection

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Simple calculator for isolated testing
type TestCalculator struct{}

func (c *TestCalculator) Calculate(input ProjectionInput) []ProjectionPoint {
	points := make([]ProjectionPoint, 0, input.PeriodMonths)

	// Convert APY to monthly rate
	annualRate := input.APY

	// Monthly compound rate calculation
	monthlyRate := decimal.Zero
	if input.CompoundFrequency == CompoundDaily {
		// For daily compounding: use simple approximation
		monthlyRate = annualRate.Div(decimal.NewFromInt(12)).Mul(decimal.NewFromFloat(1.05)) // Slightly higher for daily
	} else {
		// For monthly compounding: APY / 12
		monthlyRate = annualRate.Div(decimal.NewFromInt(12))
	}

	balance := input.InitialDeposit
	totalDeposited := input.InitialDeposit

	for month := 1; month <= input.PeriodMonths; month++ {
		// Add monthly contribution at the beginning of the month
		if month > 1 {
			balance = balance.Add(input.MonthlyContribution)
			totalDeposited = totalDeposited.Add(input.MonthlyContribution)
		}

		// Apply compound interest
		interestEarned := balance.Mul(monthlyRate)
		balance = balance.Add(interestEarned)

		// Calculate total yield earned
		totalYield := balance.Sub(totalDeposited)

		points = append(points, ProjectionPoint{
			Month:     month,
			Principal: totalDeposited,
			Yield:     totalYield,
			Total:     balance,
		})
	}

	return points
}

func TestProjectionCalculation(t *testing.T) {
	calculator := &TestCalculator{}

	input := ProjectionInput{
		InitialDeposit:      decimal.NewFromInt(1000),
		MonthlyContribution: decimal.Zero,
		APY:                 decimal.NewFromFloat(0.12), // 12% APY
		PeriodMonths:        12,
		CompoundFrequency:   CompoundMonthly,
	}

	err := input.Validate()
	require.NoError(t, err, "Input should be valid")

	results := calculator.Calculate(input)

	require.Len(t, results, 12, "Should return exactly 12 months")

	// Test first month
	first := results[0]
	assert.Equal(t, 1, first.Month)
	assert.True(t, first.Principal.Equal(decimal.NewFromInt(1000)))
	assert.True(t, first.Yield.GreaterThan(decimal.Zero))
	assert.True(t, first.Total.GreaterThan(first.Principal))

	// Test growth trend
	for i := 1; i < len(results); i++ {
		assert.True(t, results[i].Total.GreaterThan(results[i-1].Total),
			"Total should increase from month %d to %d", i, i+1)
	}

	// Test final result is reasonable (should be around 1000 * 1.12 = 1120 for 12% APY)
	final := results[11]
	finalValue := final.Total.InexactFloat64()
	assert.Greater(t, finalValue, 1100.0, "Final value should be over 1100")
	assert.Less(t, finalValue, 1150.0, "Final value should be under 1150")
}

func TestProjectionWithMonthlyContributions(t *testing.T) {
	calculator := &TestCalculator{}

	input := ProjectionInput{
		InitialDeposit:      decimal.NewFromInt(1000),
		MonthlyContribution: decimal.NewFromInt(200),
		APY:                 decimal.NewFromFloat(0.10), // 10% APY
		PeriodMonths:        6,
		CompoundFrequency:   CompoundMonthly,
	}

	results := calculator.Calculate(input)

	require.Len(t, results, 6)

	// Check principal accumulates correctly
	month2 := results[1]
	expectedPrincipal := decimal.NewFromInt(1200) // 1000 + 200
	assert.True(t, month2.Principal.Equal(expectedPrincipal))

	finalMonth := results[5]
	expectedFinalPrincipal := decimal.NewFromInt(2000) // 1000 + 5*200
	assert.True(t, finalMonth.Principal.Equal(expectedFinalPrincipal))

	// Yield should be positive
	assert.True(t, finalMonth.Yield.GreaterThan(decimal.Zero))
}

func TestProjectionInputValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   ProjectionInput
		wantErr bool
	}{
		{
			name: "Valid input",
			input: ProjectionInput{
				InitialDeposit:      decimal.NewFromInt(1000),
				MonthlyContribution: decimal.NewFromInt(100),
				APY:                 decimal.NewFromFloat(0.08),
				PeriodMonths:        12,
				CompoundFrequency:   CompoundMonthly,
			},
			wantErr: false,
		},
		{
			name: "Zero initial deposit - should fail",
			input: ProjectionInput{
				InitialDeposit:      decimal.Zero,
				MonthlyContribution: decimal.NewFromInt(100),
				APY:                 decimal.NewFromFloat(0.08),
				PeriodMonths:        12,
				CompoundFrequency:   CompoundMonthly,
			},
			wantErr: true,
		},
		{
			name: "Negative monthly contribution - should fail",
			input: ProjectionInput{
				InitialDeposit:      decimal.NewFromInt(1000),
				MonthlyContribution: decimal.NewFromInt(-50),
				APY:                 decimal.NewFromFloat(0.08),
				PeriodMonths:        12,
				CompoundFrequency:   CompoundMonthly,
			},
			wantErr: true,
		},
		{
			name: "Zero APY - should fail",
			input: ProjectionInput{
				InitialDeposit:      decimal.NewFromInt(1000),
				MonthlyContribution: decimal.Zero,
				APY:                 decimal.Zero,
				PeriodMonths:        12,
				CompoundFrequency:   CompoundMonthly,
			},
			wantErr: true,
		},
		{
			name: "Zero period - should fail",
			input: ProjectionInput{
				InitialDeposit:      decimal.NewFromInt(1000),
				MonthlyContribution: decimal.Zero,
				APY:                 decimal.NewFromFloat(0.08),
				PeriodMonths:        0,
				CompoundFrequency:   CompoundMonthly,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCompoundFrequency(t *testing.T) {
	tests := []struct {
		input    string
		expected CompoundFrequency
		wantErr  bool
	}{
		{"daily", CompoundDaily, false},
		{"monthly", CompoundMonthly, false},
		{"invalid", CompoundMonthly, true},
		{"", CompoundMonthly, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseCompoundFrequency(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}

	// Test periods per year
	assert.Equal(t, 365, CompoundDaily.PeriodsPerYear())
	assert.Equal(t, 12, CompoundMonthly.PeriodsPerYear())

	// Test string representation
	assert.Equal(t, "daily", CompoundDaily.String())
	assert.Equal(t, "monthly", CompoundMonthly.String())
}

// -------------------------------------------------------------------------
// Monte Carlo savings forecasting (#843)
// -------------------------------------------------------------------------

// TestRunMonteCarloSimulation_DeterministicGivenSeed is referenced by name in
// DefaultPathCount's doc comment: identical MonteCarloParams (in particular
// an identical Seed) must always produce bit-for-bit identical percentile
// bands, which is what makes caching/reproducing a result across API
// replicas safe.
func TestRunMonteCarloSimulation_DeterministicGivenSeed(t *testing.T) {
	params := MonteCarloParams{
		InitialDeposit:              decimal.NewFromInt(1000),
		MonthlyContribution:         decimal.NewFromInt(100),
		ExpectedAPY:                 decimal.NewFromFloat(0.08),
		APYStdDev:                   decimal.NewFromFloat(0.05),
		PeriodMonths:                24,
		CompoundFrequency:           CompoundMonthly,
		ContributionSkipProbability: 0.1,
		ContributionVariationPct:    0.1,
		PathCount:                   DefaultPathCount,
		Seed:                        DeriveSeed("vault-a", "1000", "100", "24"),
	}

	first := RunMonteCarloSimulation(params)
	second := RunMonteCarloSimulation(params)

	require.Equal(t, len(first.Timeline), len(second.Timeline))
	for i := range first.Timeline {
		assert.Truef(t, first.Timeline[i].P10.Equal(second.Timeline[i].P10), "month %d P10 mismatch: %s vs %s", i+1, first.Timeline[i].P10, second.Timeline[i].P10)
		assert.Truef(t, first.Timeline[i].P50.Equal(second.Timeline[i].P50), "month %d P50 mismatch: %s vs %s", i+1, first.Timeline[i].P50, second.Timeline[i].P50)
		assert.Truef(t, first.Timeline[i].P90.Equal(second.Timeline[i].P90), "month %d P90 mismatch: %s vs %s", i+1, first.Timeline[i].P90, second.Timeline[i].P90)
	}
	assert.True(t, first.FinalBand.P10.Equal(second.FinalBand.P10))
	assert.True(t, first.FinalBand.P50.Equal(second.FinalBand.P50))
	assert.True(t, first.FinalBand.P90.Equal(second.FinalBand.P90))
	assert.Equal(t, first.Seed, second.Seed)

	// A different seed (different stable inputs) must not collide with this
	// one's band, or determinism would be vacuous.
	other := params
	other.Seed = DeriveSeed("vault-b", "1000", "100", "24")
	third := RunMonteCarloSimulation(other)
	assert.False(t, first.FinalBand.P50.Equal(third.FinalBand.P50) && first.FinalBand.P10.Equal(third.FinalBand.P10) && first.FinalBand.P90.Equal(third.FinalBand.P90),
		"different seeds produced an identical band; RNG streams are not actually varying with the seed")
}

// TestRunMonteCarloSimulation_ZeroVolatilityCollapsesToDeterministicProjection
// checks that with APYStdDev=0 and no contribution skip/variation, every
// path (and therefore every percentile) collapses onto the same number a
// hand-rolled deterministic replica of the engine's own per-month formula
// produces. This is what guarantees the Monte Carlo engine is a strict
// generalization of the existing deterministic calculator, not a different
// model that happens to look similar.
func TestRunMonteCarloSimulation_ZeroVolatilityCollapsesToDeterministicProjection(t *testing.T) {
	initial := decimal.NewFromInt(1000)
	contribution := decimal.NewFromInt(200)
	apy := decimal.NewFromFloat(0.06)
	months := 12

	params := MonteCarloParams{
		InitialDeposit:              initial,
		MonthlyContribution:         contribution,
		ExpectedAPY:                 apy,
		APYStdDev:                   decimal.Zero,
		PeriodMonths:                months,
		CompoundFrequency:           CompoundMonthly,
		ContributionSkipProbability: 0,
		ContributionVariationPct:    0,
		PathCount:                   MinPathCount,
		Seed:                        DeriveSeed("zero-volatility-test"),
	}

	result := RunMonteCarloSimulation(params)
	require.Len(t, result.Timeline, months)

	// Hand-replicate the deterministic path using the same monthly-rate
	// conversion the engine uses (monthlyRateFromAnnual). With APYStdDev=0
	// every path draws the same yield every month, and with
	// ContributionSkipProbability=0/ContributionVariationPct=0 every
	// contribution happens at full size on every path, so this loop is
	// exactly what every one of the engine's paths computes.
	rate := monthlyRateFromAnnual(apy.InexactFloat64(), CompoundMonthly)
	balance := initial.InexactFloat64()
	for m := 1; m <= months; m++ {
		if m > 1 {
			balance += contribution.InexactFloat64()
		}
		balance += balance * rate
	}
	expected := decimal.NewFromFloat(balance).Round(2)

	assert.Truef(t, result.FinalBand.P10.Equal(expected), "P10: expected %s, got %s", expected, result.FinalBand.P10)
	assert.Truef(t, result.FinalBand.P50.Equal(expected), "P50: expected %s, got %s", expected, result.FinalBand.P50)
	assert.Truef(t, result.FinalBand.P90.Equal(expected), "P90: expected %s, got %s", expected, result.FinalBand.P90)
}

// TestRunMonteCarloSimulation_GoalSuccessProbabilityMatchesHandComputedCase
// exercises a case simple enough to compute by hand: with zero APY and zero
// initial deposit, the only contribution opportunity within a 2-month
// horizon is month 2 (the engine never adds a contribution in month 1 - see
// the "if m > 1" gate in RunMonteCarloSimulation). A path reaches the $100
// target at month 2 if and only if that single contribution isn't skipped,
// which happens with probability 1 - ContributionSkipProbability. With
// enough independent paths the sampled probability should land within a few
// standard errors of that value.
func TestRunMonteCarloSimulation_GoalSuccessProbabilityMatchesHandComputedCase(t *testing.T) {
	target := decimal.NewFromInt(100)
	targetMonth := 2
	skipProb := 0.3
	pathCount := 5000

	params := MonteCarloParams{
		InitialDeposit:              decimal.Zero,
		MonthlyContribution:         decimal.NewFromInt(100),
		ExpectedAPY:                 decimal.Zero,
		APYStdDev:                   decimal.Zero,
		PeriodMonths:                2,
		CompoundFrequency:           CompoundMonthly,
		ContributionSkipProbability: skipProb,
		ContributionVariationPct:    0,
		PathCount:                   pathCount,
		Seed:                        DeriveSeed("goal-success-hand-computed"),
		TargetAmount:                &target,
		TargetMonth:                 &targetMonth,
	}

	result := RunMonteCarloSimulation(params)
	require.NotNil(t, result.GoalSuccess)
	assert.True(t, result.GoalSuccess.TargetAmount.Equal(target))
	assert.Equal(t, targetMonth, result.GoalSuccess.DeadlineMonths)

	// Hand computation: P(success) = 1 - skipProb = 0.7. Standard error of
	// the sampled proportion at n=5000 is sqrt(0.7*0.3/5000) ~= 0.0065, so a
	// 0.03 tolerance is >4 standard errors - comfortably tight while not
	// flaking on ordinary sampling noise.
	expected := 1 - skipProb
	assert.InDelta(t, expected, result.GoalSuccess.Probability, 0.03)
}

// TestSensitivityGrid_MoreDepositNeverLowersSuccessProbability is referenced
// by name in SensitivityGrid's doc comment: because every grid cell reuses
// the same Seed (common random numbers), increasing the monthly contribution
// can only ever add to a path's balance, so success probability must be
// non-decreasing in contribution for a fixed deadline. This is required to
// hold as an exact guarantee, not merely "usually" - hence asserting across
// every deadline column and a contribution range that spans negative deltas
// too.
func TestSensitivityGrid_MoreDepositNeverLowersSuccessProbability(t *testing.T) {
	target := decimal.NewFromInt(5000)
	targetMonth := 24

	base := MonteCarloParams{
		InitialDeposit:              decimal.NewFromInt(500),
		MonthlyContribution:         decimal.NewFromInt(150),
		ExpectedAPY:                 decimal.NewFromFloat(0.07),
		APYStdDev:                   decimal.NewFromFloat(0.15),
		PeriodMonths:                24,
		CompoundFrequency:           CompoundMonthly,
		ContributionSkipProbability: 0.2,
		ContributionVariationPct:    0.2,
		PathCount:                   3000,
		Seed:                        DeriveSeed("sensitivity-grid-monotonic"),
		TargetAmount:                &target,
		TargetMonth:                 &targetMonth,
	}

	// Ascending order matters: the assertion below walks each
	// deadline-delta column in this order and requires probability to never
	// decrease.
	contributionDeltas := []decimal.Decimal{
		decimal.NewFromInt(-100),
		decimal.NewFromInt(-50),
		decimal.Zero,
		decimal.NewFromInt(50),
		decimal.NewFromInt(100),
		decimal.NewFromInt(200),
	}
	deadlineDeltas := []int{-6, 0, 6}

	grid := SensitivityGrid(base, contributionDeltas, deadlineDeltas)
	require.Len(t, grid, len(contributionDeltas)*len(deadlineDeltas))

	byDeadline := map[int][]float64{}
	labels := map[int][]string{}
	for _, pt := range grid {
		byDeadline[pt.DeadlineMonthsDelta] = append(byDeadline[pt.DeadlineMonthsDelta], pt.SuccessProbability)
		labels[pt.DeadlineMonthsDelta] = append(labels[pt.DeadlineMonthsDelta], pt.MonthlyContributionDelta.String())
	}

	for _, dd := range deadlineDeltas {
		probs := byDeadline[dd]
		require.Len(t, probs, len(contributionDeltas))
		for i := 1; i < len(probs); i++ {
			assert.GreaterOrEqualf(t, probs[i], probs[i-1],
				"deadline delta %d: success probability decreased from contribution delta %s (%.4f) to %s (%.4f)",
				dd, labels[dd][i-1], probs[i-1], labels[dd][i], probs[i])
		}
	}
}

// TestSimulationInput_Validate_RejectsExcessivePeriod is a regression test
// for a CodeQL finding on this PR: PeriodMonths/DeadlineMonths fed an
// unbounded caller-supplied value straight into
// RunMonteCarloSimulation's make([]T, months) allocations. Validate must
// reject anything beyond MaxPeriodMonths with a clear error rather than
// silently accepting it (RunMonteCarloSimulation itself now also clamps
// defensively -- see the next test).
func TestSimulationInput_Validate_RejectsExcessivePeriod(t *testing.T) {
	base := SimulationInput{
		InitialDeposit:      decimal.NewFromInt(1000),
		MonthlyContribution: decimal.NewFromInt(100),
		APY:                 decimalPtr(decimal.NewFromFloat(0.08)),
		CompoundFrequency:   CompoundMonthly,
	}

	tooLong := base
	tooLong.PeriodMonths = MaxPeriodMonths + 1
	assert.ErrorIs(t, tooLong.Validate(), ErrPeriodTooLong)

	// A hostile/buggy caller supplying billions of months (this is exactly
	// the shape of value that made make([][]float64, months) a
	// memory-exhaustion vector before this fix).
	extreme := base
	extreme.PeriodMonths = 2_000_000_000
	assert.ErrorIs(t, extreme.Validate(), ErrPeriodTooLong)

	okDeadline := 12
	tooLongDeadline := base
	tooLongDeadline.PeriodMonths = 12
	d := MaxPeriodMonths + 1
	tooLongDeadline.DeadlineMonths = &d
	assert.ErrorIs(t, tooLongDeadline.Validate(), ErrPeriodTooLong)

	valid := base
	valid.PeriodMonths = MaxPeriodMonths
	valid.DeadlineMonths = &okDeadline
	assert.NoError(t, valid.Validate())
}

// TestRunMonteCarloSimulation_RejectsExcessivePeriodMonths proves the engine
// itself -- not just Validate -- refuses to size an allocation off a raw
// caller-supplied PeriodMonths, so any caller that reaches
// RunMonteCarloSimulation without going through Validate first (e.g. a
// future internal caller) is still safe. Uses an extreme value that would
// be a multi-gigabyte/OOM allocation if unbounded; the test passing quickly
// and without an out-of-memory failure is itself the assertion.
func TestRunMonteCarloSimulation_RejectsExcessivePeriodMonths(t *testing.T) {
	result := RunMonteCarloSimulation(MonteCarloParams{
		InitialDeposit:      decimal.NewFromInt(1000),
		MonthlyContribution: decimal.NewFromInt(100),
		ExpectedAPY:         decimal.NewFromFloat(0.08),
		APYStdDev:           decimal.NewFromFloat(0.02),
		PeriodMonths:        2_000_000_000,
		CompoundFrequency:   CompoundMonthly,
		PathCount:           MinPathCount,
		Seed:                1,
	})

	require.Len(t, result.Timeline, 0)
	assert.Zero(t, result.PathCount)
}

func decimalPtr(d decimal.Decimal) *decimal.Decimal { return &d }
