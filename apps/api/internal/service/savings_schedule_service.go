package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsschedule"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

type SavingsScheduleService struct {
	scheduleRepo savingsschedule.Repository
	goalRepo     savingsgoal.Repository
	vaultRepo    vault.Repository
	minDeposit   decimal.Decimal
	clock        func() time.Time
}

func NewSavingsScheduleService(
	scheduleRepo savingsschedule.Repository,
	goalRepo savingsgoal.Repository,
	vaultRepo vault.Repository,
	minDeposit decimal.Decimal,
) *SavingsScheduleService {
	return &SavingsScheduleService{
		scheduleRepo: scheduleRepo,
		goalRepo:     goalRepo,
		vaultRepo:    vaultRepo,
		minDeposit:   minDeposit,
		clock:        func() time.Time { return time.Now().UTC() },
	}
}

func (s *SavingsScheduleService) SetClock(clock func() time.Time) {
	s.clock = clock
}

type CreateSavingsScheduleInput struct {
	Amount    decimal.Decimal `json:"amount"`
	Currency  string          `json:"currency"`
	Frequency string          `json:"frequency"`
	VaultID   uuid.UUID       `json:"vault_id"`
}

func (s *SavingsScheduleService) Create(
	ctx context.Context,
	userID, goalID uuid.UUID,
	in CreateSavingsScheduleInput,
) (savingsschedule.SavingsSchedule, error) {
	goal, err := s.goalRepo.GetByID(ctx, goalID)
	if err != nil {
		return savingsschedule.SavingsSchedule{}, err
	}
	if goal.UserID != userID {
		return savingsschedule.SavingsSchedule{}, savingsgoal.ErrGoalNotFound
	}

	frequency, err := savingsschedule.ParseFrequency(in.Frequency)
	if err != nil {
		return savingsschedule.SavingsSchedule{}, err
	}

	if err := validateScheduleAmount(in.Amount, s.minDeposit); err != nil {
		return savingsschedule.SavingsSchedule{}, err
	}

	v, err := s.vaultRepo.GetVault(ctx, in.VaultID)
	if err != nil {
		return savingsschedule.SavingsSchedule{}, err
	}
	if v.UserID != userID {
		return savingsschedule.SavingsSchedule{}, savingsschedule.ErrUnauthorizedVault
	}

	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "USDC"
	}

	now := s.clock()
	schedule := &savingsschedule.SavingsSchedule{
		ID:        uuid.New(),
		UserID:    userID,
		GoalID:    goalID,
		VaultID:   in.VaultID,
		Amount:    in.Amount,
		Currency:  currency,
		Frequency: frequency,
		NextRunAt: now,
		IsActive:  true,
	}
	if err := s.scheduleRepo.Create(ctx, schedule); err != nil {
		return savingsschedule.SavingsSchedule{}, err
	}
	return *schedule, nil
}

func (s *SavingsScheduleService) List(
	ctx context.Context,
	userID, goalID uuid.UUID,
) ([]savingsschedule.SavingsSchedule, error) {
	goal, err := s.goalRepo.GetByID(ctx, goalID)
	if err != nil {
		return nil, err
	}
	if goal.UserID != userID {
		return nil, savingsgoal.ErrGoalNotFound
	}
	schedules, err := s.scheduleRepo.ListByGoal(ctx, goalID, userID)
	if err != nil {
		return nil, err
	}
	if schedules == nil {
		schedules = []savingsschedule.SavingsSchedule{}
	}
	return schedules, nil
}

func (s *SavingsScheduleService) Cancel(
	ctx context.Context,
	userID, goalID, scheduleID uuid.UUID,
) error {
	goal, err := s.goalRepo.GetByID(ctx, goalID)
	if err != nil {
		return err
	}
	if goal.UserID != userID {
		return savingsgoal.ErrGoalNotFound
	}
	return s.scheduleRepo.Cancel(ctx, scheduleID, goalID, userID)
}

func validateScheduleAmount(amount, minDeposit decimal.Decimal) error {
	if amount.Cmp(decimal.Zero) <= 0 {
		return fmt.Errorf("%w: amount must be positive", savingsschedule.ErrInvalidSchedule)
	}
	if minDeposit.IsPositive() && amount.Cmp(minDeposit) <= 0 {
		return fmt.Errorf("%w: amount must exceed minimum deposit of %s", savingsschedule.ErrInvalidSchedule, minDeposit)
	}
	if decimalScale(amount) > vault.MaxAmountScale {
		return fmt.Errorf("%w: amount precision exceeds %d decimal places", savingsschedule.ErrInvalidSchedule, vault.MaxAmountScale)
	}
	return nil
}
