package projection

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrInvalidProjectionInput = errors.New("invalid projection input")
	ErrInvalidAmount          = errors.New("amount must be greater than zero")
	ErrInvalidPeriod          = errors.New("period must be greater than zero")
	ErrInvalidAPY             = errors.New("APY must be greater than zero")
	// ErrPeriodTooLong is returned when a caller-supplied period/deadline
	// exceeds MaxPeriodMonths (simulation.go). Rejecting it here, rather than
	// only clamping deep inside the simulation engine, gives the caller a
	// clear error instead of a silently-truncated result.
	ErrPeriodTooLong = errors.New("period exceeds the maximum supported number of months")
)

// CompoundFrequency represents how often compound interest is calculated
type CompoundFrequency int

const (
	CompoundDaily CompoundFrequency = iota
	CompoundMonthly
)

// String returns the string representation of CompoundFrequency
func (f CompoundFrequency) String() string {
	switch f {
	case CompoundDaily:
		return "daily"
	case CompoundMonthly:
		return "monthly"
	default:
		return "unknown"
	}
}

// PeriodsPerYear returns the number of compounding periods per year
func (f CompoundFrequency) PeriodsPerYear() int {
	switch f {
	case CompoundDaily:
		return 365
	case CompoundMonthly:
		return 12
	default:
		return 12
	}
}

// ParseCompoundFrequency converts a string to CompoundFrequency
func ParseCompoundFrequency(s string) (CompoundFrequency, error) {
	switch s {
	case "daily":
		return CompoundDaily, nil
	case "monthly":
		return CompoundMonthly, nil
	default:
		return CompoundMonthly, errors.New("invalid compound frequency")
	}
}

// ProjectionInput represents the input parameters for compound interest calculation
type ProjectionInput struct {
	InitialDeposit      decimal.Decimal   `json:"initial_deposit"`
	MonthlyContribution decimal.Decimal   `json:"monthly_contribution"`
	APY                 decimal.Decimal   `json:"apy"`
	PeriodMonths        int               `json:"period_months"`
	CompoundFrequency   CompoundFrequency `json:"compound_frequency"`
}

// Validate checks if the projection input is valid
func (p *ProjectionInput) Validate() error {
	if p.InitialDeposit.LessThanOrEqual(decimal.Zero) {
		return ErrInvalidAmount
	}
	if p.MonthlyContribution.LessThan(decimal.Zero) {
		return errors.New("monthly contribution cannot be negative")
	}
	if p.APY.LessThanOrEqual(decimal.Zero) {
		return ErrInvalidAPY
	}
	if p.PeriodMonths <= 0 {
		return ErrInvalidPeriod
	}
	return nil
}

// ProjectionPoint represents a single point in the compound interest projection
type ProjectionPoint struct {
	Month     int             `json:"month"`
	Principal decimal.Decimal `json:"principal"`
	Yield     decimal.Decimal `json:"yield"`
	Total     decimal.Decimal `json:"total"`
}

// ProjectionOutput represents the result of a compound interest calculation
type ProjectionOutput struct {
	VaultID      *uuid.UUID        `json:"vault_id,omitempty"`
	Currency     string            `json:"currency"`
	CurrentAPY   float64           `json:"current_apy"`
	Input        ProjectionInput   `json:"input"`
	Timeline     []ProjectionPoint `json:"timeline"`
	Summary      ProjectionSummary `json:"summary"`
	CalculatedAt time.Time         `json:"calculated_at"`
}

// ProjectionSummary provides aggregate statistics about the projection
type ProjectionSummary struct {
	TotalDeposited decimal.Decimal `json:"total_deposited"`
	TotalYield     decimal.Decimal `json:"total_yield"`
	FinalBalance   decimal.Decimal `json:"final_balance"`
	EffectiveAPY   decimal.Decimal `json:"effective_apy"`
}

// VaultProjectionInput represents input for vault-specific projections
type VaultProjectionInput struct {
	VaultID           uuid.UUID        `json:"vault_id"`
	Deposit           decimal.Decimal  `json:"deposit"`
	Period            string           `json:"period"`
	CompoundFrequency string           `json:"compound"`
	APYOverride       *decimal.Decimal `json:"apy_override,omitempty"`
}

// Service defines the interface for projection calculations
type Service interface {
	CalculateCompoundProjection(ctx context.Context, input ProjectionInput) (*ProjectionOutput, error)
	CalculateVaultProjection(ctx context.Context, input VaultProjectionInput) (*ProjectionOutput, error)
}

// Calculator handles the core compound interest mathematics
type Calculator interface {
	Calculate(input ProjectionInput) []ProjectionPoint
}
