package savingsgoal

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrGoalNotFound    = errors.New("savings goal not found")
	ErrInvalidGoal     = errors.New("invalid savings goal")
	ErrUnauthorized    = errors.New("unauthorized")
)

type SavingsGoal struct {
	ID            uuid.UUID       `json:"id"`
	UserID        uuid.UUID       `json:"user_id"`
	TargetAmount  decimal.Decimal `json:"target_amount"`
	Currency      string          `json:"currency"`
	Deadline      time.Time       `json:"deadline"`
	Description   string          `json:"description,omitempty"`
	CurrentAmount decimal.Decimal `json:"current_amount"`
	ProgressPct   float64         `json:"progress_pct"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type Repository interface {
	Create(ctx context.Context, goal *SavingsGoal) error
	ListByUser(ctx context.Context, userID uuid.UUID) ([]SavingsGoal, error)
	GetByID(ctx context.Context, id uuid.UUID) (*SavingsGoal, error)
	Update(ctx context.Context, goal *SavingsGoal) error
	Delete(ctx context.Context, id, userID uuid.UUID) error
	SumVaultBalance(ctx context.Context, userID uuid.UUID, currency string) (decimal.Decimal, error)
}
