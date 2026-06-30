package savingsgoal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrGoalNotFound = errors.New("savings goal not found")
	ErrInvalidGoal  = errors.New("invalid savings goal")
	ErrUnauthorized = errors.New("unauthorized")
	// ErrGoalCompleted is returned when an operation is not allowed on a goal
	// that has already reached its target (e.g. changing its deadline).
	ErrGoalCompleted = errors.New("savings goal already completed")
	// ErrGoalPaused is returned when an action requires the goal to be active.
	ErrGoalPaused = errors.New("savings goal is paused")
	// ErrGoalArchived is returned when an operation is not allowed on an archived goal
	// (e.g. adding a new contribution schedule).
	ErrGoalArchived = errors.New("savings goal is archived")
)

const (
	GoalStatusActive    = "active"
	GoalStatusPaused    = "paused"
	GoalStatusCompleted = "completed"
	// GoalStatusArchived is a terminal state a user can move a goal into (e.g.
	// after it completes) to hide it without deleting it (#684).
	GoalStatusArchived = "archived"
)

// ParseStatusFilter validates a status used to filter the goal list. An empty
// value means "no filter" and returns ("", nil).
func ParseStatusFilter(value string) (string, error) {
	switch status := strings.ToLower(strings.TrimSpace(value)); status {
	case "":
		return "", nil
	case GoalStatusActive, GoalStatusPaused, GoalStatusCompleted, GoalStatusArchived:
		return status, nil
	default:
		return "", fmt.Errorf("%w: invalid status", ErrInvalidGoal)
	}
}

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
	// ActiveGoalCount counts goals in "active" status only (excludes archived, paused, completed).
	ActiveGoalCount    int `json:"active_goal_count"`
	CompletedGoalCount int `json:"completed_goal_count"`
	// OverallProgressPct is USDC progress (saved/target, capped at 100). USDC
	// only, to avoid cross-currency conversion the rest of this type avoids.
	OverallProgressPct float64 `json:"overall_progress_pct"`
	// NextDeadline is the nearest future deadline across active goals, or nil.
	// No omitempty so it serializes as JSON null when absent.
	NextDeadline *time.Time `json:"next_deadline"`
	// CurrentStreak is the number of consecutive weeks the user made at least one deposit.
	CurrentStreak int `json:"current_streak"`
	// LongestStreak is the user's all-time best consecutive-week streak.
	LongestStreak int `json:"longest_streak"`
}

// ValidateEmoji checks that s is a single emoji character (or empty).
// Returns ErrInvalidGoal if s contains non-emoji runes.
func ValidateEmoji(s string) error {
	if s == "" {
		return nil
	}
	runes := []rune(s)
	for _, r := range runes {
		if !unicode.Is(unicode.So, r) && !unicode.Is(unicode.Sm, r) &&
			!unicode.Is(unicode.Sk, r) && !isEmojiRange(r) {
			return fmt.Errorf("%w: emoji must contain only emoji characters", ErrInvalidGoal)
		}
	}
	return nil
}

func isEmojiRange(r rune) bool {
	return (r >= 0x1F600 && r <= 0x1F64F) || // emoticons
		(r >= 0x1F300 && r <= 0x1F5FF) || // misc symbols
		(r >= 0x1F680 && r <= 0x1F6FF) || // transport
		(r >= 0x1F700 && r <= 0x1F77F) || // alchemical
		(r >= 0x1F780 && r <= 0x1F7FF) || // geometric shapes extended
		(r >= 0x1F800 && r <= 0x1F8FF) || // supplemental arrows
		(r >= 0x1F900 && r <= 0x1F9FF) || // supplemental symbols
		(r >= 0x1FA00 && r <= 0x1FA6F) || // chess symbols
		(r >= 0x1FA70 && r <= 0x1FAFF) || // symbols and pictographs extended
		(r >= 0x2600 && r <= 0x26FF) ||   // misc symbols
		(r >= 0x2700 && r <= 0x27BF) ||   // dingbats
		r == 0xFE0F ||                     // variation selector-16
		(r >= 0x1F1E0 && r <= 0x1F1FF) || // flags
		(r >= 0x200D && r <= 0x200D)       // zero-width joiner
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
	// Name is a user-supplied display label (#738). Defaults to first 50 chars of Description.
	Name  string `json:"name,omitempty"`
	// Emoji is a single Unicode emoji icon (#738).
	Emoji string `json:"emoji,omitempty"`
	Category      GoalCategory    `json:"category"`
	// Status is one of "active", "paused", "completed" (#718, #716).
	Status             string          `json:"status"`
	CurrentAmount      decimal.Decimal `json:"current_amount"`
	ProgressPct        float64         `json:"progress_pct"`
	NotifiedMilestones []int           `json:"-"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	// Completion fields (#716).
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	CompletionAction string    `json:"completion_action,omitempty"`
	// Velocity stats (#714).
	AvgWeeklyDeposit       decimal.Decimal `json:"avg_weekly_deposit"`
	ProjectedDaysToComplete *int            `json:"projected_days_to_completion,omitempty"`
	OnTrack                bool            `json:"on_track"`
	// Sharing fields.
	ShareToken      *uuid.UUID `json:"share_token,omitempty"`
	ShareEnabledAt  *time.Time `json:"share_enabled_at,omitempty"`
	IsShared        bool       `json:"is_shared"`
}

// SharedGoalView is the read-only public projection of a savings goal exposed
// via the unauthenticated share link. It deliberately omits user PII.
type SharedGoalView struct {
	Name        string          `json:"name"`
	Emoji       string          `json:"emoji,omitempty"`
	TargetAmount decimal.Decimal `json:"target_amount"`
	Currency    string          `json:"currency"`
	CurrentAmount decimal.Decimal `json:"current_amount"`
	ProgressPct float64         `json:"progress_pct"`
	Deadline    time.Time       `json:"deadline"`
	Category    GoalCategory    `json:"category"`
	Status      string          `json:"status"`
}

// GoalDeposit records a single allocation in a multi-goal deposit split (#719).
type GoalDeposit struct {
	ID       uuid.UUID
	GoalID   uuid.UUID
	UserID   uuid.UUID
	Amount   decimal.Decimal
	Currency string
}

type Repository interface {
	Create(ctx context.Context, goal *SavingsGoal) error
	ListByUser(ctx context.Context, userID uuid.UUID, category string) ([]SavingsGoal, error)
	GetByID(ctx context.Context, id uuid.UUID) (*SavingsGoal, error)
	Update(ctx context.Context, goal *SavingsGoal) error
	Delete(ctx context.Context, id, userID uuid.UUID) error
	SumVaultBalance(ctx context.Context, userID uuid.UUID, currency string) (decimal.Decimal, error)
	UpdateMilestones(ctx context.Context, goalID uuid.UUID, milestones []int) error
	// SumRecentDeposits sums vault deposit amounts for the user in the given currency since `since` (#714).
	SumRecentDeposits(ctx context.Context, userID uuid.UUID, currency string, since time.Time) (decimal.Decimal, error)
	// UpdateStatus sets the goal's status column (#718).
	UpdateStatus(ctx context.Context, goalID, userID uuid.UUID, status string) error
	// MarkCompleted sets completed_at and completion_action (#716).
	MarkCompleted(ctx context.Context, goalID, userID uuid.UUID, action string) error
	// RecordGoalDeposits atomically inserts all deposits in a single DB transaction (#719).
	RecordGoalDeposits(ctx context.Context, deposits []GoalDeposit) error
	// SumGoalDeposits returns the total deposited to a specific goal via deposit-split (#719).
	SumGoalDeposits(ctx context.Context, goalID uuid.UUID) (decimal.Decimal, error)
	// SetShareToken enables public sharing by persisting a UUID token on the goal.
	SetShareToken(ctx context.Context, goalID, userID uuid.UUID, token uuid.UUID) error
	// ClearShareToken removes the share token, revoking public access.
	ClearShareToken(ctx context.Context, goalID, userID uuid.UUID) error
	// GetByShareToken returns the goal whose share_token matches. Returns ErrGoalNotFound if none.
	GetByShareToken(ctx context.Context, token uuid.UUID) (*SavingsGoal, error)
}
