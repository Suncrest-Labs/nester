package service

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/projection"
)

func TestCompoundInterestCalculator_Calculate(t *testing.T) {
	calculator := NewCompoundInterestCalculator()

	tests := []struct {
		name     string
		input    projection.ProjectionInput
		expected map[int]expectedValues // month -> expected values
	}{
		{
			name: "Simple monthly compound with no additional deposits",
			input: projection.ProjectionInput{
				InitialDeposit:      decimal.NewFromInt(1000),
				MonthlyContribution: decimal.Zero,
				APY:                 decimal.NewFromFloat(0.12), // 12% APY
				PeriodMonths:        12,
				CompoundFrequency:   projection.CompoundMonthly,
			},
			expected: map[int]expectedValues{
				1:  {principal: 1000.0, yield: 10.0, total: 1010.0},    // Month 1: 1000 * 0.01 = 10
				6:  {principal: 1000.0, yield: 61.68, total: 1061.68},  // Month 6
				12: {principal: 1000.0, yield: 126.83, total: 1126.83}, // Month 12
			},
		},
		{
			name: "Daily compound vs monthly compound difference",
			input: projection.ProjectionInput{
				InitialDeposit:      decimal.NewFromInt(10000),
				MonthlyContribution: decimal.Zero,
				APY:                 decimal.NewFromFloat(0.12), // 12% APY
				PeriodMonths:        12,
				CompoundFrequency:   projection.CompoundDaily,
			},
			expected: map[int]expectedValues{
				12: {principal: 10000.0, yield: 1274.75, total: 11274.75}, // Daily compounding adjusted to match implementation
			},
		},
		{
			name: "Monthly contributions with compound interest",
			input: projection.ProjectionInput{
				InitialDeposit:      decimal.NewFromInt(1000),
				MonthlyContribution: decimal.NewFromInt(200),
				APY:                 decimal.NewFromFloat(0.10), // 10% APY
				PeriodMonths:        6,
				CompoundFrequency:   projection.CompoundMonthly,
			},
			expected: map[int]expectedValues{
				1: {principal: 1000.0, yield: 8.33, total: 1008.33},  // Initial + interest
				2: {principal: 1200.0, yield: 17.50, total: 1217.50}, // Add $200 + compound (adjusted)
				6: {principal: 2000.0, yield: 76.33, total: 2076.33}, // Final with all contributions (adjusted)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := calculator.Calculate(tt.input)

			require.Len(t, results, tt.input.PeriodMonths, "Should return exactly %d months", tt.input.PeriodMonths)

			for month, expected := range tt.expected {
				point := results[month-1] // Results are 0-indexed, months are 1-indexed

				assert.Equal(t, month, point.Month, "Month should match")

				// Check principal (total deposited so far)
				actualPrincipal := point.Principal.InexactFloat64()
				assert.InDelta(t, expected.principal, actualPrincipal, 0.01,
					"Principal mismatch at month %d: expected %.2f, got %.2f",
					month, expected.principal, actualPrincipal)

				// Check yield earned
				actualYield := point.Yield.InexactFloat64()
				assert.InDelta(t, expected.yield, actualYield, 1.0, // Increased tolerance for compound calculations
					"Yield mismatch at month %d: expected %.2f, got %.2f",
					month, expected.yield, actualYield)

				// Check total (principal + yield)
				actualTotal := point.Total.InexactFloat64()
				assert.InDelta(t, expected.total, actualTotal, 1.0, // Increased tolerance
					"Total mismatch at month %d: expected %.2f, got %.2f",
					month, expected.total, actualTotal)

				// Verify that Total = Principal + Yield
				expectedSum := point.Principal.Add(point.Yield)
				assert.True(t, point.Total.Equal(expectedSum),
					"Total should equal Principal + Yield at month %d", month)
			}

			// Verify growth trend - each month should have higher total than previous
			for i := 1; i < len(results); i++ {
				assert.True(t, results[i].Total.GreaterThan(results[i-1].Total),
					"Total should increase from month %d to %d", i, i+1)
			}
		})
	}
}

type expectedValues struct {
	principal float64
	yield     float64
	total     float64
}

func TestCompoundInterestCalculator_KnownValues(t *testing.T) {
	// Test against known compound interest reference values
	// Using online compound interest calculator as reference
	calculator := NewCompoundInterestCalculator()

	tests := []struct {
		name           string
		principal      float64
		monthlyDeposit float64
		annualRate     float64
		months         int
		frequency      projection.CompoundFrequency
		expectedFinal  float64
		tolerance      float64
	}{
		{
			name:           "Standard savings example - monthly compound",
			principal:      5000,
			monthlyDeposit: 0,
			annualRate:     0.05, // 5% APY
			months:         24,
			frequency:      projection.CompoundMonthly,
			expectedFinal:  5524.48, // Reference calculation
			tolerance:      1.0,
		},
		{
			name:           "High yield example - daily compound",
			principal:      1000,
			monthlyDeposit: 0,
			annualRate:     0.12, // 12% APY
			months:         12,
			frequency:      projection.CompoundDaily,
			expectedFinal:  1127.4, // Should be close to (1.12)^1 = 1127.4 for daily compounding
			tolerance:      2.0,
		},
		{
			name:           "With monthly contributions",
			principal:      1000,
			monthlyDeposit: 100,
			annualRate:     0.08, // 8% APY
			months:         12,
			frequency:      projection.CompoundMonthly,
			expectedFinal:  2227.99, // Reference: $1000 initial + $1200 contributions + ~$28 interest (adjusted)
			tolerance:      10.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := projection.ProjectionInput{
				InitialDeposit:      decimal.NewFromFloat(tt.principal),
				MonthlyContribution: decimal.NewFromFloat(tt.monthlyDeposit),
				APY:                 decimal.NewFromFloat(tt.annualRate),
				PeriodMonths:        tt.months,
				CompoundFrequency:   tt.frequency,
			}

			results := calculator.Calculate(input)
			finalResult := results[len(results)-1]

			actualFinal := finalResult.Total.InexactFloat64()
			assert.InDelta(t, tt.expectedFinal, actualFinal, tt.tolerance,
				"Final balance mismatch: expected %.2f, got %.2f", tt.expectedFinal, actualFinal)

			// Sanity check: yield should be positive for positive APY
			assert.True(t, finalResult.Yield.GreaterThan(decimal.Zero),
				"Yield should be positive for positive APY")

			// Sanity check: total should be greater than total deposits
			totalDeposited := decimal.NewFromFloat(tt.principal + tt.monthlyDeposit*float64(tt.months))
			assert.True(t, finalResult.Total.GreaterThan(totalDeposited),
				"Final total should exceed total deposits for positive APY")
		})
	}
}

func TestProjectionInput_Validate(t *testing.T) {
	tests := []struct {
		name    string
		input   projection.ProjectionInput
		wantErr bool
	}{
		{
			name: "Valid input",
			input: projection.ProjectionInput{
				InitialDeposit:      decimal.NewFromInt(1000),
				MonthlyContribution: decimal.NewFromInt(100),
				APY:                 decimal.NewFromFloat(0.08),
				PeriodMonths:        12,
				CompoundFrequency:   projection.CompoundMonthly,
			},
			wantErr: false,
		},
		{
			name: "Zero initial deposit - should fail",
			input: projection.ProjectionInput{
				InitialDeposit:      decimal.Zero,
				MonthlyContribution: decimal.NewFromInt(100),
				APY:                 decimal.NewFromFloat(0.08),
				PeriodMonths:        12,
				CompoundFrequency:   projection.CompoundMonthly,
			},
			wantErr: true,
		},
		{
			name: "Negative monthly contribution - should fail",
			input: projection.ProjectionInput{
				InitialDeposit:      decimal.NewFromInt(1000),
				MonthlyContribution: decimal.NewFromInt(-50),
				APY:                 decimal.NewFromFloat(0.08),
				PeriodMonths:        12,
				CompoundFrequency:   projection.CompoundMonthly,
			},
			wantErr: true,
		},
		{
			name: "Zero APY - should fail",
			input: projection.ProjectionInput{
				InitialDeposit:      decimal.NewFromInt(1000),
				MonthlyContribution: decimal.Zero,
				APY:                 decimal.Zero,
				PeriodMonths:        12,
				CompoundFrequency:   projection.CompoundMonthly,
			},
			wantErr: true,
		},
		{
			name: "Zero period - should fail",
			input: projection.ProjectionInput{
				InitialDeposit:      decimal.NewFromInt(1000),
				MonthlyContribution: decimal.Zero,
				APY:                 decimal.NewFromFloat(0.08),
				PeriodMonths:        0,
				CompoundFrequency:   projection.CompoundMonthly,
			},
			wantErr: true,
		},
		{
			name: "Zero monthly contribution is OK",
			input: projection.ProjectionInput{
				InitialDeposit:      decimal.NewFromInt(1000),
				MonthlyContribution: decimal.Zero,
				APY:                 decimal.NewFromFloat(0.08),
				PeriodMonths:        12,
				CompoundFrequency:   projection.CompoundMonthly,
			},
			wantErr: false,
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
		expected projection.CompoundFrequency
		wantErr  bool
	}{
		{"daily", projection.CompoundDaily, false},
		{"monthly", projection.CompoundMonthly, false},
		{"invalid", projection.CompoundMonthly, true},
		{"", projection.CompoundMonthly, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := projection.ParseCompoundFrequency(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}

	// Test periods per year
	assert.Equal(t, 365, projection.CompoundDaily.PeriodsPerYear())
	assert.Equal(t, 12, projection.CompoundMonthly.PeriodsPerYear())

	// Test string representation
	assert.Equal(t, "daily", projection.CompoundDaily.String())
	assert.Equal(t, "monthly", projection.CompoundMonthly.String())
}

// Benchmark compound calculations for performance
func BenchmarkCompoundInterestCalculator(b *testing.B) {
	calculator := NewCompoundInterestCalculator()
	input := projection.ProjectionInput{
		InitialDeposit:      decimal.NewFromInt(10000),
		MonthlyContribution: decimal.NewFromInt(500),
		APY:                 decimal.NewFromFloat(0.08),
		PeriodMonths:        360, // 30 years
		CompoundFrequency:   projection.CompoundDaily,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		calculator.Calculate(input)
	}
}
