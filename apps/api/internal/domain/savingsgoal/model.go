package savingsgoal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrGoalNotFound = errors.New("savings goal not found")
	ErrInvalidGoal  = errors.New("invalid savings goal")
	ErrUnauthorized = errors.New("unauthorized")
)

type GoalCategory string

const (
	CategoryEmergencyFund GoalCategory = "emergency_fund"
	CategoryEducation     GoalCategory = "education"
	CategoryHousing       GoalCategory = "housing"
	CategoryTravel        GoalCategory = "travel"
	CategoryBusiness      GoalCategory = "business"
	CategoryHealth        GoalCategory = "health"
	CategoryRetirement    GoalCategory = "retirement"
	CategoryOther         GoalCategory = "other"
)

const (
	CurrencyUSDC = "USDC"
	CurrencyXLM  = "XLM"
)

// SupportedCurrencies lists goal denominations accepted by the savings API.
var SupportedCurrencies = map[string]bool{
	CurrencyUSDC: true,
	CurrencyXLM:  true,
}

// IsSupportedCurrency reports whether currency is a supported savings goal denomination.
func IsSupportedCurrency(currency string) bool {
	return SupportedCurrencies[NormalizeCurrency(currency)]
}

// NormalizeCurrency uppercases and trims a currency code for storage and comparison.
func NormalizeCurrency(currency string) string {
	return strings.ToUpper(strings.TrimSpace(currency))
}

// SavingsGoalsSummary aggregates goal totals per currency without cross-currency conversion.
type SavingsGoalsSummary struct {
	TotalSavedUSDC  decimal.Decimal `json:"total_saved_usdc"`
	TotalTargetUSDC decimal.Decimal `json:"total_target_usdc"`
	TotalSavedXLM   decimal.Decimal `json:"total_saved_xlm"`
	TotalTargetXLM  decimal.Decimal `json:"total_target_xlm"`
	GoalCount       int             `json:"goal_count"`
}

func ParseCategory(value string) (GoalCategory, error) {
	category := GoalCategory(strings.ToLower(strings.TrimSpace(value)))
	switch category {
	case CategoryEmergencyFund,
		CategoryEducation,
		CategoryHousing,
		CategoryTravel,
		CategoryBusiness,
		CategoryHealth,
		CategoryRetirement,
		CategoryOther:
		return category, nil
	default:
		return "", fmt.Errorf("%w: invalid category", ErrInvalidGoal)
	}
}

type SavingsGoal struct {
	ID            uuid.UUID       `json:"id"`
	UserID        uuid.UUID       `json:"user_id"`
	TargetAmount  decimal.Decimal `json:"target_amount"`
	Currency      string          `json:"currency"`
	Deadline      time.Time       `json:"deadline"`
	Description   string          `json:"description,omitempty"`
	Category      GoalCategory    `json:"category"`
	CurrentAmount decimal.Decimal `json:"current_amount"`
	ProgressPct   float64         `json:"progress_pct"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type Repository interface {
	Create(ctx context.Context, goal *SavingsGoal) error
	ListByUser(ctx context.Context, userID uuid.UUID, category string) ([]SavingsGoal, error)
	GetByID(ctx context.Context, id uuid.UUID) (*SavingsGoal, error)
	Update(ctx context.Context, goal *SavingsGoal) error
	Delete(ctx context.Context, id, userID uuid.UUID) error
	SumVaultBalance(ctx context.Context, userID uuid.UUID, currency string) (decimal.Decimal, error)
}
