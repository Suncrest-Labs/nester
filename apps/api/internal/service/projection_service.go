package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/performance"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/projection"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsschedule"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// ProjectionService handles compound interest projections, including the
// Monte Carlo savings forecast added for issue #843 (see
// projection_simulation.go). savingsGoalRepo/savingsScheduleRepo are only
// used by the Monte Carlo path (to resolve a goal's target/deadline and a
// user's real contribution cadence); the deterministic methods below don't
// touch them.
type ProjectionService struct {
	calculator      projection.Calculator
	vaultRepo       vault.Repository
	performanceRepo performance.SnapshotRepository

	savingsGoalRepo     savingsgoal.Repository
	savingsScheduleRepo savingsschedule.Repository
	simCache            *simulationCache
}

// NewProjectionService creates a new projection service
func NewProjectionService(
	calculator projection.Calculator,
	vaultRepo vault.Repository,
	performanceRepo performance.SnapshotRepository,
	savingsGoalRepo savingsgoal.Repository,
	savingsScheduleRepo savingsschedule.Repository,
) *ProjectionService {
	return &ProjectionService{
		calculator:          calculator,
		vaultRepo:           vaultRepo,
		performanceRepo:     performanceRepo,
		savingsGoalRepo:     savingsGoalRepo,
		savingsScheduleRepo: savingsScheduleRepo,
		simCache:            newSimulationCache(SimulationCacheTTL),
	}
}

// CalculateCompoundProjection calculates compound interest projection for generic input
func (s *ProjectionService) CalculateCompoundProjection(ctx context.Context, input projection.ProjectionInput) (*projection.ProjectionOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	timeline := s.calculator.Calculate(input)
	if len(timeline) == 0 {
		return nil, errors.New("failed to calculate projection")
	}

	summary := s.calculateSummary(input, timeline)

	output := &projection.ProjectionOutput{
		Currency:     "USD",
		CurrentAPY:   input.APY.InexactFloat64(),
		Input:        input,
		Timeline:     timeline,
		Summary:      summary,
		CalculatedAt: time.Now(),
	}

	return output, nil
}

// CalculateVaultProjection calculates projection for a specific vault
func (s *ProjectionService) CalculateVaultProjection(ctx context.Context, input projection.VaultProjectionInput) (*projection.ProjectionOutput, error) {
	// Get vault to check it exists and is accessible
	vaultEntity, err := s.vaultRepo.GetVault(ctx, input.VaultID)
	if err != nil {
		return nil, fmt.Errorf("failed to get vault: %w", err)
	}

	// Parse period (e.g., "12m" -> 12 months)
	periodMonths, err := parsePeriod(input.Period)
	if err != nil {
		return nil, fmt.Errorf("invalid period format: %w", err)
	}

	// Parse compound frequency
	compoundFreq, err := projection.ParseCompoundFrequency(input.CompoundFrequency)
	if err != nil {
		return nil, fmt.Errorf("invalid compound frequency: %w", err)
	}

	// Get APY - use override if provided, otherwise get from performance data
	apy := input.APYOverride
	if apy == nil {
		vaultAPY, err := s.getCurrentAPY(ctx, input.VaultID)
		if err != nil {
			// If we can't get historical APY, use a default rate
			defaultAPY := decimal.NewFromFloat(0.05) // 5% default
			apy = &defaultAPY
		} else {
			apy = &vaultAPY
		}
	}

	// Build projection input
	projInput := projection.ProjectionInput{
		InitialDeposit:      input.Deposit,
		MonthlyContribution: decimal.Zero, // Single deposit for now
		APY:                 *apy,
		PeriodMonths:        periodMonths,
		CompoundFrequency:   compoundFreq,
	}

	if err := projInput.Validate(); err != nil {
		return nil, err
	}

	timeline := s.calculator.Calculate(projInput)
	if len(timeline) == 0 {
		return nil, errors.New("failed to calculate projection")
	}

	summary := s.calculateSummary(projInput, timeline)

	output := &projection.ProjectionOutput{
		VaultID:      &input.VaultID,
		Currency:     vaultEntity.Currency,
		CurrentAPY:   apy.InexactFloat64(),
		Input:        projInput,
		Timeline:     timeline,
		Summary:      summary,
		CalculatedAt: time.Now(),
	}

	return output, nil
}

// getCurrentAPY retrieves the current APY for a vault from performance data
func (s *ProjectionService) getCurrentAPY(ctx context.Context, vaultID uuid.UUID) (decimal.Decimal, error) {
	// Try to get the latest APY records
	apyRecords, err := s.performanceRepo.ListAPY(ctx, vaultID)
	if err != nil {
		return decimal.Zero, err
	}

	// Find the most recent 30-day APY
	for _, record := range apyRecords {
		if record.Period == performance.Period30d {
			return record.RealizedAPY, nil
		}
	}

	// Fall back to any available APY
	if len(apyRecords) > 0 {
		return apyRecords[0].RealizedAPY, nil
	}

	return decimal.Zero, errors.New("no APY data available")
}

// calculateSummary computes summary statistics from the timeline
func (s *ProjectionService) calculateSummary(input projection.ProjectionInput, timeline []projection.ProjectionPoint) projection.ProjectionSummary {
	if len(timeline) == 0 {
		return projection.ProjectionSummary{}
	}

	lastPoint := timeline[len(timeline)-1]
	totalDeposited := input.InitialDeposit.Add(input.MonthlyContribution.Mul(decimal.NewFromInt(int64(input.PeriodMonths))))

	// Calculate effective APY
	years := decimal.NewFromInt(int64(input.PeriodMonths)).Div(decimal.NewFromInt(12))
	effectiveAPY := decimal.Zero
	if years.GreaterThan(decimal.Zero) && totalDeposited.GreaterThan(decimal.Zero) {
		// Effective APY = (Final Balance / Total Deposited)^(1/years) - 1
		ratio := lastPoint.Total.Div(totalDeposited)
		if ratio.GreaterThan(decimal.Zero) {
			// Use approximation for fractional exponents
			effectiveAPYFloat := math.Pow(ratio.InexactFloat64(), 1.0/years.InexactFloat64()) - 1.0
			effectiveAPY = decimal.NewFromFloat(effectiveAPYFloat)
		}
	}

	return projection.ProjectionSummary{
		TotalDeposited: totalDeposited,
		TotalYield:     lastPoint.Yield,
		FinalBalance:   lastPoint.Total,
		EffectiveAPY:   effectiveAPY,
	}
}

// parsePeriod converts period strings like "12m", "24", to months
func parsePeriod(period string) (int, error) {
	period = strings.TrimSpace(strings.ToLower(period))

	if strings.HasSuffix(period, "m") {
		monthsStr := strings.TrimSuffix(period, "m")
		return strconv.Atoi(monthsStr)
	}

	// If no suffix, assume months
	return strconv.Atoi(period)
}

// CompoundInterestCalculator implements the Calculator interface
type CompoundInterestCalculator struct{}

// NewCompoundInterestCalculator creates a new calculator
func NewCompoundInterestCalculator() *CompoundInterestCalculator {
	return &CompoundInterestCalculator{}
}

// Calculate implements the compound interest formula with monthly contributions
func (c *CompoundInterestCalculator) Calculate(input projection.ProjectionInput) []projection.ProjectionPoint {
	// Security: Limit the maximum period to prevent excessive memory allocation
	const maxPeriodMonths = 1200 // 100 years maximum
	if input.PeriodMonths > maxPeriodMonths {
		// Return empty slice for invalid input rather than panicking
		return []projection.ProjectionPoint{}
	}

	// Additional security check for negative values
	if input.PeriodMonths <= 0 {
		return []projection.ProjectionPoint{}
	}

	points := make([]projection.ProjectionPoint, 0, input.PeriodMonths)

	// Convert APY to monthly rate
	annualRate := input.APY

	// Monthly compound rate calculation
	monthlyRate := decimal.Zero
	if input.CompoundFrequency == projection.CompoundDaily {
		// For daily compounding: (1 + APY/365)^(365/12) - 1
		dailyRate := annualRate.Div(decimal.NewFromInt(365))
		onePlusDailyRate := decimal.NewFromInt(1).Add(dailyRate)

		// Approximate (1 + r)^(365/12) ≈ (1 + r)^30.42
		compoundsPerMonth := decimal.NewFromFloat(365.0 / 12.0)
		monthlyMultiplier := math.Pow(onePlusDailyRate.InexactFloat64(), compoundsPerMonth.InexactFloat64())
		monthlyRate = decimal.NewFromFloat(monthlyMultiplier).Sub(decimal.NewFromInt(1))
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

		points = append(points, projection.ProjectionPoint{
			Month:     month,
			Principal: totalDeposited,
			Yield:     totalYield,
			Total:     balance,
		})
	}

	return points
}
