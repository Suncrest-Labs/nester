package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsschedule"
)

type SavingsScheduleRepository struct {
	db *sql.DB
}

func NewSavingsScheduleRepository(db *sql.DB) *SavingsScheduleRepository {
	return &SavingsScheduleRepository{db: db}
}

func (r *SavingsScheduleRepository) Create(ctx context.Context, schedule *savingsschedule.SavingsSchedule) error {
	query := `
		INSERT INTO savings_schedules (
			id, user_id, goal_id, vault_id, amount, currency, frequency, next_run_at, is_active
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING created_at, updated_at
	`
	err := r.db.QueryRowContext(
		ctx, query,
		schedule.ID,
		schedule.UserID,
		schedule.GoalID,
		schedule.VaultID,
		schedule.Amount.String(),
		schedule.Currency,
		string(schedule.Frequency),
		schedule.NextRunAt,
		schedule.IsActive,
	).Scan(&schedule.CreatedAt, &schedule.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
			strings.Contains(pgErr.ConstraintName, "idx_savings_schedules_one_active_per_goal") {
			return savingsschedule.ErrActiveScheduleExists
		}
		return err
	}
	return nil
}

func (r *SavingsScheduleRepository) ListByGoal(ctx context.Context, goalID, userID uuid.UUID) ([]savingsschedule.SavingsSchedule, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, goal_id, vault_id, amount, currency, frequency,
		       next_run_at, last_run_at, is_active, created_at, updated_at
		FROM savings_schedules
		WHERE goal_id = $1 AND user_id = $2
		ORDER BY created_at DESC
	`, goalID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []savingsschedule.SavingsSchedule
	for rows.Next() {
		s, err := scanSavingsSchedule(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, s)
	}
	return schedules, rows.Err()
}

func (r *SavingsScheduleRepository) GetByID(ctx context.Context, scheduleID uuid.UUID) (*savingsschedule.SavingsSchedule, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, goal_id, vault_id, amount, currency, frequency,
		       next_run_at, last_run_at, is_active, created_at, updated_at
		FROM savings_schedules WHERE id = $1
	`, scheduleID)
	s, err := scanSavingsSchedule(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, savingsschedule.ErrScheduleNotFound
		}
		return nil, err
	}
	return &s, nil
}

func (r *SavingsScheduleRepository) Cancel(ctx context.Context, scheduleID, goalID, userID uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE savings_schedules
		SET is_active = FALSE, updated_at = NOW()
		WHERE id = $1 AND goal_id = $2 AND user_id = $3 AND is_active = TRUE
	`, scheduleID, goalID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsschedule.ErrScheduleNotFound
	}
	return nil
}

func (r *SavingsScheduleRepository) ListDue(ctx context.Context, now time.Time) ([]savingsschedule.SavingsSchedule, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, goal_id, vault_id, amount, currency, frequency,
		       next_run_at, last_run_at, is_active, created_at, updated_at
		FROM savings_schedules
		WHERE is_active = TRUE AND next_run_at <= $1
		ORDER BY next_run_at ASC
	`, now.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []savingsschedule.SavingsSchedule
	for rows.Next() {
		s, err := scanSavingsSchedule(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, s)
	}
	return schedules, rows.Err()
}

func (r *SavingsScheduleRepository) UpdateAfterRun(ctx context.Context, id uuid.UUID, lastRunAt, nextRunAt time.Time) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE savings_schedules
		SET last_run_at = $2, next_run_at = $3, updated_at = NOW()
		WHERE id = $1 AND is_active = TRUE
	`, id, lastRunAt.UTC(), nextRunAt.UTC())
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsschedule.ErrScheduleNotFound
	}
	return nil
}

func (r *SavingsScheduleRepository) Deactivate(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE savings_schedules
		SET is_active = FALSE, updated_at = NOW()
		WHERE id = $1
	`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsschedule.ErrScheduleNotFound
	}
	return nil
}

type savingsScheduleScanner interface {
	Scan(dest ...any) error
}

func scanSavingsSchedule(row savingsScheduleScanner) (savingsschedule.SavingsSchedule, error) {
	var (
		id, userID, goalID, vaultID, amountStr, currency, frequency string
		nextRunAt, createdAt, updatedAt                            time.Time
		lastRunAt                                                  sql.NullTime
		isActive                                                   bool
	)
	if err := row.Scan(
		&id, &userID, &goalID, &vaultID, &amountStr, &currency, &frequency,
		&nextRunAt, &lastRunAt, &isActive, &createdAt, &updatedAt,
	); err != nil {
		return savingsschedule.SavingsSchedule{}, err
	}
	amount, _ := decimal.NewFromString(amountStr)
	parsedID, _ := uuid.Parse(id)
	parsedUserID, _ := uuid.Parse(userID)
	parsedGoalID, _ := uuid.Parse(goalID)
	parsedVaultID, _ := uuid.Parse(vaultID)

	var lastRunPtr *time.Time
	if lastRunAt.Valid {
		t := lastRunAt.Time.UTC()
		lastRunPtr = &t
	}

	return savingsschedule.SavingsSchedule{
		ID:        parsedID,
		UserID:    parsedUserID,
		GoalID:    parsedGoalID,
		VaultID:   parsedVaultID,
		Amount:    amount,
		Currency:  currency,
		Frequency: savingsschedule.Frequency(frequency),
		NextRunAt: nextRunAt.UTC(),
		LastRunAt: lastRunPtr,
		IsActive:  isActive,
		CreatedAt: createdAt.UTC(),
		UpdatedAt: updatedAt.UTC(),
	}, nil
}
