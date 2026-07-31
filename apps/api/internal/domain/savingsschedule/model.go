package savingsschedule

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
	ErrScheduleNotFound      = errors.New("savings schedule not found")
	ErrInvalidSchedule       = errors.New("invalid savings schedule")
	ErrActiveScheduleExists  = errors.New("an active schedule already exists for this goal")
	ErrUnauthorizedVault     = errors.New("vault does not belong to user")
)

type Frequency string

const (
	FrequencyWeekly   Frequency = "weekly"
	FrequencyBiweekly Frequency = "biweekly"
	FrequencyMonthly  Frequency = "monthly"
)

func ParseFrequency(value string) (Frequency, error) {
	f := Frequency(strings.ToLower(strings.TrimSpace(value)))
	switch f {
	case FrequencyWeekly, FrequencyBiweekly, FrequencyMonthly:
		return f, nil
	default:
		return "", fmt.Errorf("%w: frequency must be weekly, biweekly, or monthly", ErrInvalidSchedule)
	}
}

type SavingsSchedule struct {
	ID               uuid.UUID       `json:"id"`
	UserID           uuid.UUID       `json:"user_id"`
	GoalID           uuid.UUID       `json:"goal_id"`
	VaultID          uuid.UUID       `json:"vault_id"`
	Amount           decimal.Decimal `json:"amount"`
	Currency         string          `json:"currency"`
	Frequency        Frequency       `json:"frequency"`
	NextRunAt        time.Time       `json:"next_run_at"`
	LastRunAt        *time.Time      `json:"last_run_at,omitempty"`
	IsActive         bool            `json:"is_active"`
	OnchainMandateID *int64          `json:"onchain_mandate_id,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type Repository interface {
	Create(ctx context.Context, schedule *SavingsSchedule) error
	ListByGoal(ctx context.Context, goalID, userID uuid.UUID) ([]SavingsSchedule, error)
	GetByID(ctx context.Context, scheduleID uuid.UUID) (*SavingsSchedule, error)
	GetActiveByGoal(ctx context.Context, goalID, userID uuid.UUID) (*SavingsSchedule, error)
	Cancel(ctx context.Context, scheduleID, goalID, userID uuid.UUID) error
	CancelActiveByGoal(ctx context.Context, goalID, userID uuid.UUID) error
	Update(ctx context.Context, schedule *SavingsSchedule) error
	ListDue(ctx context.Context, now time.Time) ([]SavingsSchedule, error)
	UpdateAfterRun(ctx context.Context, id uuid.UUID, lastRunAt, nextRunAt time.Time) error
	Deactivate(ctx context.Context, id uuid.UUID) error
}
