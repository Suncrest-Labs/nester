package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
)

type SavingsGoalRepository struct {
	db *sql.DB
}

func NewSavingsGoalRepository(db *sql.DB) *SavingsGoalRepository {
	return &SavingsGoalRepository{db: db}
}

func (r *SavingsGoalRepository) Create(ctx context.Context, goal *savingsgoal.SavingsGoal) error {
	query := `
		INSERT INTO savings_goals (id, user_id, target_amount, currency, deadline, description, category)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at, status
	`
	return r.db.QueryRowContext(
		ctx, query,
		goal.ID, goal.UserID, goal.TargetAmount.String(), goal.Currency, goal.Deadline, nullSQLString(goal.Description), string(goal.Category),
	).Scan(&goal.CreatedAt, &goal.UpdatedAt, &goal.Status)
}

func (r *SavingsGoalRepository) ListByUser(ctx context.Context, userID uuid.UUID, category string) ([]savingsgoal.SavingsGoal, error) {
	query := `
		SELECT id, user_id, target_amount, currency, deadline, description, category,
		       notified_milestones, created_at, updated_at,
		       status, completed_at, completion_action
		FROM savings_goals
		WHERE user_id = $1
	`
	args := []any{userID}
	if category != "" {
		query += ` AND category = $2`
		args = append(args, category)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var goals []savingsgoal.SavingsGoal
	for rows.Next() {
		g, err := scanSavingsGoal(rows)
		if err != nil {
			return nil, err
		}
		goals = append(goals, g)
	}
	return goals, rows.Err()
}

func (r *SavingsGoalRepository) GetByID(ctx context.Context, id uuid.UUID) (*savingsgoal.SavingsGoal, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, target_amount, currency, deadline, description, category,
		       notified_milestones, created_at, updated_at,
		       status, completed_at, completion_action
		FROM savings_goals WHERE id = $1
	`, id)
	g, err := scanSavingsGoal(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, savingsgoal.ErrGoalNotFound
		}
		return nil, err
	}
	return &g, nil
}

func (r *SavingsGoalRepository) Update(ctx context.Context, goal *savingsgoal.SavingsGoal) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE savings_goals
		SET target_amount = $1, currency = $2, deadline = $3, description = $4, category = $5, updated_at = NOW()
		WHERE id = $6 AND user_id = $7
	`, goal.TargetAmount.String(), goal.Currency, goal.Deadline, nullSQLString(goal.Description), string(goal.Category), goal.ID, goal.UserID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsgoal.ErrGoalNotFound
	}
	return nil
}

func (r *SavingsGoalRepository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM savings_goals WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsgoal.ErrGoalNotFound
	}
	return nil
}

func (r *SavingsGoalRepository) SumVaultBalance(ctx context.Context, userID uuid.UUID, currency string) (decimal.Decimal, error) {
	var total sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(current_balance), 0)
		FROM vaults
		WHERE user_id = $1 AND deleted_at IS NULL AND status = 'active'
		  AND ($2 = '' OR currency = $2)
	`, userID, currency).Scan(&total)
	if err != nil {
		return decimal.Zero, err
	}
	if !total.Valid || total.String == "" {
		return decimal.Zero, nil
	}
	return decimal.NewFromString(total.String)
}

func (r *SavingsGoalRepository) UpdateMilestones(ctx context.Context, goalID uuid.UUID, milestones []int) error {
	if milestones == nil {
		milestones = []int{}
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE savings_goals
		SET notified_milestones = $1, updated_at = NOW()
		WHERE id = $2
	`, pq.Array(milestones), goalID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsgoal.ErrGoalNotFound
	}
	return nil
}

func (r *SavingsGoalRepository) UpdateStatus(ctx context.Context, goalID, userID uuid.UUID, status string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE savings_goals SET status = $1, updated_at = NOW()
		WHERE id = $2 AND user_id = $3
	`, status, goalID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsgoal.ErrGoalNotFound
	}
	return nil
}

func (r *SavingsGoalRepository) MarkCompleted(ctx context.Context, goalID, userID uuid.UUID, action string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE savings_goals
		SET status = 'completed', completed_at = NOW(), completion_action = $1, updated_at = NOW()
		WHERE id = $2 AND user_id = $3
	`, action, goalID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsgoal.ErrGoalNotFound
	}
	return nil
}

// MarkArchived sets status=archived and records archived_at timestamp (#721).
func (r *SavingsGoalRepository) MarkArchived(ctx context.Context, goalID, userID uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE savings_goals
		SET status = 'archived', archived_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND user_id = $2
	`, goalID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsgoal.ErrGoalNotFound
	}
	return nil
}

// MarkUnarchived restores an archived goal to active status and clears archived_at (#721).
func (r *SavingsGoalRepository) MarkUnarchived(ctx context.Context, goalID, userID uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE savings_goals
		SET status = 'active', archived_at = NULL, updated_at = NOW()
		WHERE id = $1 AND user_id = $2
	`, goalID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsgoal.ErrGoalNotFound
	}
	return nil
}

func (r *SavingsGoalRepository) SumRecentDeposits(ctx context.Context, userID uuid.UUID, currency string, since time.Time) (decimal.Decimal, error) {
	var total sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(vt.amount), 0)
		FROM vault_transactions vt
		JOIN vaults v ON vt.vault_id = v.id
		WHERE v.user_id = $1
		  AND v.currency = $2
		  AND vt.type = 'deposit'
		  AND vt.created_at >= $3
		  AND v.deleted_at IS NULL
	`, userID, currency, since).Scan(&total)
	if err != nil {
		return decimal.Zero, err
	}
	if !total.Valid || total.String == "" {
		return decimal.Zero, nil
	}
	return decimal.NewFromString(total.String)
}

type savingsGoalScanner interface {
	Scan(dest ...any) error
}

func scanSavingsGoal(row savingsGoalScanner) (savingsgoal.SavingsGoal, error) {
	var (
		id, userID, targetStr, currency, category string
		deadline, createdAt, updatedAt            time.Time
		description                               sql.NullString
		notifiedMilestones                        pq.Int32Array
		status                                    sql.NullString
		completedAt                               sql.NullTime
		completionAction                          sql.NullString
	)
	if err := row.Scan(
		&id, &userID, &targetStr, &currency, &deadline, &description, &category,
		&notifiedMilestones, &createdAt, &updatedAt,
		&status, &completedAt, &completionAction,
	); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	parsedID, _ := uuid.Parse(id)
	parsedUserID, _ := uuid.Parse(userID)
	target, _ := decimal.NewFromString(targetStr)
	desc := ""
	if description.Valid {
		desc = description.String
	}
	milestones := make([]int, 0, len(notifiedMilestones))
	for _, m := range notifiedMilestones {
		milestones = append(milestones, int(m))
	}
	goalStatus := savingsgoal.GoalStatusActive
	if status.Valid && status.String != "" {
		goalStatus = status.String
	}
	var completedAtPtr *time.Time
	if completedAt.Valid {
		t := completedAt.Time
		completedAtPtr = &t
	}
	return savingsgoal.SavingsGoal{
		ID:                  parsedID,
		UserID:              parsedUserID,
		TargetAmount:        target,
		Currency:            currency,
		Deadline:            deadline,
		Description:         desc,
		Category:            savingsgoal.GoalCategory(category),
		Status:              goalStatus,
		NotifiedMilestones:  milestones,
		CreatedAt:           createdAt,
		UpdatedAt:           updatedAt,
		CompletedAt:         completedAtPtr,
		CompletionAction:    completionAction.String,
	}, nil
}

func nullSQLString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
