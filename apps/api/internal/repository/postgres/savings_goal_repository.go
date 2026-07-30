package postgres

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/pkg/listquery"
)

type SavingsGoalRepository struct {
	db *sql.DB
}

func NewSavingsGoalRepository(db *sql.DB) *SavingsGoalRepository {
	return &SavingsGoalRepository{db: db}
}

func (r *SavingsGoalRepository) Create(ctx context.Context, goal *savingsgoal.SavingsGoal) error {
	query := `
		INSERT INTO savings_goals (id, user_id, vault_id, target_amount, currency, deadline, description, category, name, emoji, min_contribution, max_contribution)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING created_at, updated_at, status, auto_compound, yield_balance
	`
	var yieldBalanceStr string
	if err := r.db.QueryRowContext(
		ctx, query,
		goal.ID, goal.UserID, nullUUID(goal.VaultID), goal.TargetAmount.String(), goal.Currency, goal.Deadline,
		nullSQLString(goal.Description), string(goal.Category),
		nullSQLString(goal.Name), nullSQLString(goal.Emoji),
		nullDecimal(goal.MinContribution), nullDecimal(goal.MaxContribution),
	).Scan(&goal.CreatedAt, &goal.UpdatedAt, &goal.Status, &goal.AutoCompound, &yieldBalanceStr); err != nil {
		return err
	}
	yieldBalance, err := decimal.NewFromString(yieldBalanceStr)
	if err != nil {
		return err
	}
	goal.YieldBalance = yieldBalance
	return nil
}

func (r *SavingsGoalRepository) SetShareToken(ctx context.Context, goalID, userID uuid.UUID, token uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE savings_goals
		SET share_token = $1, share_enabled_at = NOW(), updated_at = NOW()
		WHERE id = $2 AND user_id = $3
	`, token, goalID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsgoal.ErrGoalNotFound
	}
	return nil
}

func (r *SavingsGoalRepository) ClearShareToken(ctx context.Context, goalID, userID uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE savings_goals
		SET share_token = NULL, share_enabled_at = NULL, updated_at = NOW()
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

func (r *SavingsGoalRepository) GetByShareToken(ctx context.Context, token uuid.UUID) (*savingsgoal.SavingsGoal, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, vault_id, target_amount, currency, deadline, description, category,
		       notified_milestones, deadline_reminders_sent, created_at, updated_at,
		       status, completed_at, completion_action, name, emoji,
		       share_token, share_enabled_at, onchain_goal_id, onchain_status,
		       min_contribution, max_contribution, deleted_at,
		       auto_compound, yield_balance
		FROM savings_goals WHERE share_token = $1 AND deleted_at IS NULL
	`, token)
	g, err := scanSavingsGoalWithShare(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, savingsgoal.ErrGoalNotFound
		}
		return nil, err
	}
	return &g, nil
}

// GetByVaultID returns the goal linked to the given vault, if any.
func (r *SavingsGoalRepository) GetByVaultID(ctx context.Context, vaultID uuid.UUID) (*savingsgoal.SavingsGoal, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, vault_id, target_amount, currency, deadline, description, category,
		       notified_milestones, deadline_reminders_sent, created_at, updated_at,
		       status, completed_at, completion_action, name, emoji,
		       share_token, share_enabled_at, onchain_goal_id, onchain_status,
		       min_contribution, max_contribution, deleted_at,
		       auto_compound, yield_balance
		FROM savings_goals WHERE vault_id = $1 AND deleted_at IS NULL
		ORDER BY created_at ASC
		LIMIT 1
	`, vaultID)
	g, err := scanSavingsGoalWithShare(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, savingsgoal.ErrGoalNotFound
		}
		return nil, err
	}
	return &g, nil
}

// CreditYieldBalance atomically adds amount to the goal's yield_balance (#task1).
func (r *SavingsGoalRepository) CreditYieldBalance(ctx context.Context, goalID uuid.UUID, amount decimal.Decimal) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE savings_goals
		SET yield_balance = yield_balance + $1::numeric, updated_at = NOW()
		WHERE id = $2
	`, amount.String(), goalID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsgoal.ErrGoalNotFound
	}
	return nil
}

func (r *SavingsGoalRepository) ListByUser(ctx context.Context, userID uuid.UUID, category, search string) ([]savingsgoal.SavingsGoal, error) {
	query := `
		SELECT id, user_id, vault_id, target_amount, currency, deadline, description, category,
		       notified_milestones, deadline_reminders_sent, created_at, updated_at,
		       status, completed_at, completion_action, name, emoji,
		       share_token, share_enabled_at, onchain_goal_id, onchain_status,
		       min_contribution, max_contribution, deleted_at,
		       auto_compound, yield_balance
		FROM savings_goals
		WHERE user_id = $1 AND deleted_at IS NULL
	`
	args := []any{userID}
	if category != "" {
		args = append(args, category)
		query += fmt.Sprintf(` AND category = $%d`, len(args))
	}
	if search != "" {
		args = append(args, search)
		query += fmt.Sprintf(` AND search_vector @@ plainto_tsquery('english', $%d)`, len(args))
	}
	query += ` ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var goals []savingsgoal.SavingsGoal
	for rows.Next() {
		g, err := scanSavingsGoalWithShare(rows)
		if err != nil {
			return nil, err
		}
		goals = append(goals, g)
	}
	return goals, rows.Err()
}

func (r *SavingsGoalRepository) GetByID(ctx context.Context, id uuid.UUID) (*savingsgoal.SavingsGoal, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, vault_id, target_amount, currency, deadline, description, category,
		       notified_milestones, deadline_reminders_sent, created_at, updated_at,
		       status, completed_at, completion_action, name, emoji,
		       share_token, share_enabled_at, onchain_goal_id, onchain_status,
		       min_contribution, max_contribution, deleted_at,
		       auto_compound, yield_balance
		FROM savings_goals WHERE id = $1 AND deleted_at IS NULL
	`, id)
	g, err := scanSavingsGoalWithShare(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, savingsgoal.ErrGoalNotFound
		}
		return nil, err
	}
	return &g, nil
}

// GetByIDIncludingDeleted looks up a goal regardless of deleted_at (#924),
// so Restore can inspect a soft-deleted goal's deleted_at to enforce the
// recovery window.
func (r *SavingsGoalRepository) GetByIDIncludingDeleted(ctx context.Context, id uuid.UUID) (*savingsgoal.SavingsGoal, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, vault_id, target_amount, currency, deadline, description, category,
		       notified_milestones, deadline_reminders_sent, created_at, updated_at,
		       status, completed_at, completion_action, name, emoji,
		       share_token, share_enabled_at, onchain_goal_id, onchain_status,
		       min_contribution, max_contribution, deleted_at,
		       auto_compound, yield_balance
		FROM savings_goals WHERE id = $1
	`, id)
	g, err := scanSavingsGoalWithShare(row)
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
		SET target_amount = $1, currency = $2, deadline = $3, description = $4, category = $5,
		    vault_id = $6, name = $7, emoji = $8, min_contribution = $9, max_contribution = $10,
		    auto_compound = $11, updated_at = NOW()
		WHERE id = $12 AND user_id = $13 AND deleted_at IS NULL
	`, goal.TargetAmount.String(), goal.Currency, goal.Deadline, nullSQLString(goal.Description),
		string(goal.Category), nullUUID(goal.VaultID), nullSQLString(goal.Name), nullSQLString(goal.Emoji),
		nullDecimal(goal.MinContribution), nullDecimal(goal.MaxContribution),
		goal.AutoCompound, goal.ID, goal.UserID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsgoal.ErrGoalNotFound
	}
	return nil
}

// Delete soft-deletes the goal by stamping deleted_at instead of destroying
// the row (#924). This is distinct from Archive (#684/#685, which flips
// status to 'archived' via UpdateStatus): a deleted goal is hidden from all
// normal reads (GetByID/ListByUser/GetByShareToken/ListActiveApproachingDeadline
// all filter on deleted_at IS NULL) but remains restorable via Restore for
// SavingsGoalRecoveryWindow, after which the scheduled purge job hard-deletes
// it. Already-deleted goals are not matched, so a repeat DELETE surfaces as
// ErrGoalNotFound (404) and no row is ever permanently removed via this path.
func (r *SavingsGoalRepository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE savings_goals
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
	`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsgoal.ErrGoalNotFound
	}
	return nil
}

// Restore clears deleted_at, undoing a soft delete (#924). The caller
// (service layer) is responsible for enforcing the recovery window before
// calling this — Restore itself only guards against restoring a goal that
// isn't actually deleted or doesn't belong to userID.
func (r *SavingsGoalRepository) Restore(ctx context.Context, id, userID uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE savings_goals
		SET deleted_at = NULL, updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NOT NULL
	`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsgoal.ErrGoalNotFound
	}
	return nil
}

// ListDeletedOlderThan returns soft-deleted goals whose deleted_at predates
// cutoff (#924), for the scheduled purge job to hard-delete.
func (r *SavingsGoalRepository) ListDeletedOlderThan(ctx context.Context, cutoff time.Time) ([]savingsgoal.SavingsGoal, error) {
	query := `
		SELECT id, user_id, vault_id, target_amount, currency, deadline, description, category,
		       notified_milestones, deadline_reminders_sent, created_at, updated_at,
		       status, completed_at, completion_action, name, emoji,
		       share_token, share_enabled_at, onchain_goal_id, onchain_status,
		       min_contribution, max_contribution, deleted_at
		FROM savings_goals
		WHERE deleted_at IS NOT NULL AND deleted_at < $1
	`
	rows, err := r.db.QueryContext(ctx, query, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var goals []savingsgoal.SavingsGoal
	for rows.Next() {
		g, err := scanSavingsGoalWithShare(rows)
		if err != nil {
			return nil, err
		}
		goals = append(goals, g)
	}
	return goals, rows.Err()
}

// HardDelete permanently removes a goal row (#924). Only called by the
// recovery-window purge job after SavingsGoalRecoveryWindow has elapsed.
func (r *SavingsGoalRepository) HardDelete(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM savings_goals WHERE id = $1 AND deleted_at IS NOT NULL`, id)
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

func (r *SavingsGoalRepository) UpdateDeadlineReminders(ctx context.Context, goalID uuid.UUID, reminders []int) error {
	if reminders == nil {
		reminders = []int{}
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE savings_goals
		SET deadline_reminders_sent = $1, updated_at = NOW()
		WHERE id = $2
	`, pq.Array(reminders), goalID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsgoal.ErrGoalNotFound
	}
	return nil
}

func (r *SavingsGoalRepository) ListActiveApproachingDeadline(ctx context.Context, maxDays int) ([]savingsgoal.SavingsGoal, error) {
	query := `
		SELECT id, user_id, vault_id, target_amount, currency, deadline, description, category,
		       notified_milestones, deadline_reminders_sent, created_at, updated_at,
		       status, completed_at, completion_action, name, emoji,
		       share_token, share_enabled_at, onchain_goal_id, onchain_status,
		       min_contribution, max_contribution, deleted_at
		FROM savings_goals
		WHERE (status = 'active' OR status IS NULL OR status = '')
		  AND deleted_at IS NULL
		  AND deadline BETWEEN NOW() AND NOW() + ($1 || ' days')::INTERVAL
	`
	rows, err := r.db.QueryContext(ctx, query, maxDays)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var goals []savingsgoal.SavingsGoal
	for rows.Next() {
		g, err := scanSavingsGoalWithShare(rows)
		if err != nil {
			return nil, err
		}
		goals = append(goals, g)
	}
	return goals, rows.Err()
}

// UpdateOnchainLink persists the result of asynchronously registering goalID
// against the savings_goal contract (#807). Not scoped to userID: this is
// called from the background registration path, not a user-facing request.
func (r *SavingsGoalRepository) UpdateOnchainLink(ctx context.Context, goalID uuid.UUID, onchainGoalID, onchainStatus string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE savings_goals
		SET onchain_goal_id = $1, onchain_status = $2, updated_at = NOW()
		WHERE id = $3
	`, onchainGoalID, onchainStatus, goalID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsgoal.ErrGoalNotFound
	}
	return nil
}

func (r *SavingsGoalRepository) ListActiveGoalUserIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT user_id
		FROM savings_goals
		WHERE (status = 'active' OR status IS NULL OR status = '')
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var idStr string
		if err := rows.Scan(&idStr); err != nil {
			return nil, err
		}
		uid, err := uuid.Parse(idStr)
		if err == nil {
			ids = append(ids, uid)
		}
	}
	return ids, rows.Err()
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

func (r *SavingsGoalRepository) ListContributions(ctx context.Context, goalID, userID uuid.UUID, params interface{}) ([]savingsgoal.GoalContribution, int, string, error) {
	pageParams, ok := params.(listquery.PageParams)
	if !ok {
		return nil, 0, "", fmt.Errorf("invalid pagination params")
	}
	if pageParams.PerPage <= 0 {
		pageParams.PerPage = listquery.DefaultPerPage
	}
	if pageParams.Page <= 0 {
		pageParams.Page = listquery.DefaultPage
	}

	countQuery := `
		SELECT COUNT(*)
		FROM vault_transactions vt
		JOIN vaults v ON vt.vault_id = v.id
		JOIN savings_schedules ss ON ss.vault_id = v.id
		WHERE ss.goal_id = $1 AND ss.user_id = $2 AND vt.type = 'deposit' AND v.deleted_at IS NULL
	`
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, goalID, userID).Scan(&total); err != nil {
		return nil, 0, "", err
	}

	query := `
               SELECT vt.id, vt.vault_id, v.user_id, vt.amount, v.currency, vt.type, vt.tx_hash, vt.created_at
               FROM vault_transactions vt
               JOIN vaults v ON vt.vault_id = v.id
               JOIN savings_schedules ss ON ss.vault_id = v.id
               WHERE ss.goal_id = $1 AND ss.user_id = $2 AND vt.type = 'deposit' AND v.deleted_at IS NULL
       `
	args := []any{goalID, userID}
	if pageParams.Cursor != "" {
		createdAt, cursorID, err := decodeGoalContributionCursor(pageParams.Cursor)
		if err != nil {
			return nil, 0, "", err
		}
		query += ` AND (vt.created_at < $3 OR (vt.created_at = $3 AND vt.id < $4))`
		args = append(args, createdAt.UTC(), cursorID)
	}
	query += fmt.Sprintf(` ORDER BY vt.created_at DESC, vt.id DESC LIMIT $%d`, len(args)+1)
	args = append(args, pageParams.PerPage+1)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, "", err
	}
	defer rows.Close()

	items := make([]savingsgoal.GoalContribution, 0, pageParams.PerPage+1)
	for rows.Next() {
		var (
			id, vaultID, userIDStr, amountStr, currency, txType string
			txHash                                              sql.NullString
			createdAt                                           time.Time
		)
		if err := rows.Scan(&id, &vaultID, &userIDStr, &amountStr, &currency, &txType, &txHash, &createdAt); err != nil {
			return nil, 0, "", err
		}
		parsedID, _ := uuid.Parse(id)
		parsedVaultID, _ := uuid.Parse(vaultID)
		parsedUserID, _ := uuid.Parse(userIDStr)
		amount, _ := decimal.NewFromString(amountStr)
		var txHashStr string
		if txHash.Valid {
			txHashStr = txHash.String
		}
		items = append(items, savingsgoal.GoalContribution{
			ID:        parsedID,
			GoalID:    goalID,
			UserID:    parsedUserID,
			Amount:    amount,
			Currency:  currency,
			Type:      txType,
			TxHash:    txHashStr,
			CreatedAt: createdAt.UTC(),
		})
		_ = parsedVaultID
	}
	if err := rows.Err(); err != nil {
		return nil, 0, "", err
	}

	nextCursor := ""
	if len(items) > pageParams.PerPage {
		last := items[pageParams.PerPage-1]
		nextCursor = encodeGoalContributionCursor(last.CreatedAt, last.ID)
		items = items[:pageParams.PerPage]
	}
	return items, total, nextCursor, nil
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

// scanSavingsGoalWithShare scans a row that includes the share_token and share_enabled_at columns.
func scanSavingsGoalWithShare(row savingsGoalScanner) (savingsgoal.SavingsGoal, error) {
	var (
		id, userID, targetStr, currency, category string
		vaultID                                   sql.NullString
		deadline, createdAt, updatedAt            time.Time
		description                               sql.NullString
		notifiedMilestones, deadlineReminders     pq.Int32Array
		status                                    sql.NullString
		completedAt                               sql.NullTime
		completionAction                          sql.NullString
		name, emoji                               sql.NullString
		shareToken                                sql.NullString
		shareEnabledAt                            sql.NullTime
		onchainGoalID, onchainStatus              sql.NullString
		minContribution, maxContribution          sql.NullString
		deletedAt                                 sql.NullTime
		autoCompound                              bool
		yieldBalanceStr                           string
	)
	if err := row.Scan(
		&id, &userID, &vaultID, &targetStr, &currency, &deadline, &description, &category,
		&notifiedMilestones, &deadlineReminders, &createdAt, &updatedAt,
		&status, &completedAt, &completionAction, &name, &emoji,
		&shareToken, &shareEnabledAt, &onchainGoalID, &onchainStatus,
		&minContribution, &maxContribution, &deletedAt,
		&autoCompound, &yieldBalanceStr,
	); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	yieldBalance, _ := decimal.NewFromString(yieldBalanceStr)
	parsedID, _ := uuid.Parse(id)
	parsedUserID, _ := uuid.Parse(userID)
	var parsedVaultID *uuid.UUID
	if vaultID.Valid {
		if v, err := uuid.Parse(vaultID.String); err == nil {
			parsedVaultID = &v
		}
	}
	target, _ := decimal.NewFromString(targetStr)
	desc := ""
	if description.Valid {
		desc = description.String
	}
	milestones := make([]int, 0, len(notifiedMilestones))
	for _, m := range notifiedMilestones {
		milestones = append(milestones, int(m))
	}
	reminders := make([]int, 0, len(deadlineReminders))
	for _, m := range deadlineReminders {
		reminders = append(reminders, int(m))
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
	var shareTokenPtr *uuid.UUID
	var shareEnabledAtPtr *time.Time
	if shareToken.Valid && shareToken.String != "" {
		parsed, err := uuid.Parse(shareToken.String)
		if err == nil {
			shareTokenPtr = &parsed
		}
	}
	if shareEnabledAt.Valid {
		t := shareEnabledAt.Time
		shareEnabledAtPtr = &t
	}
	var onchainGoalIDPtr, onchainStatusPtr *string
	if onchainGoalID.Valid {
		onchainGoalIDPtr = &onchainGoalID.String
	}
	if onchainStatus.Valid {
		onchainStatusPtr = &onchainStatus.String
	}
	var minContributionPtr, maxContributionPtr *decimal.Decimal
	if minContribution.Valid {
		if v, err := decimal.NewFromString(minContribution.String); err == nil {
			minContributionPtr = &v
		}
	}
	if maxContribution.Valid {
		if v, err := decimal.NewFromString(maxContribution.String); err == nil {
			maxContributionPtr = &v
		}
	}
	var deletedAtPtr *time.Time
	if deletedAt.Valid {
		t := deletedAt.Time
		deletedAtPtr = &t
	}
	return savingsgoal.SavingsGoal{
		ID:                    parsedID,
		UserID:                parsedUserID,
		VaultID:               parsedVaultID,
		TargetAmount:          target,
		Currency:              currency,
		Deadline:              deadline,
		Description:           desc,
		Name:                  name.String,
		Emoji:                 emoji.String,
		Category:              savingsgoal.GoalCategory(category),
		Status:                goalStatus,
		NotifiedMilestones:    milestones,
		DeadlineRemindersSent: reminders,
		CreatedAt:             createdAt,
		UpdatedAt:             updatedAt,
		CompletedAt:           completedAtPtr,
		CompletionAction:      completionAction.String,
		ShareToken:            shareTokenPtr,
		ShareEnabledAt:        shareEnabledAtPtr,
		IsShared:              shareTokenPtr != nil,
		OnchainGoalID:         onchainGoalIDPtr,
		OnchainStatus:         onchainStatusPtr,
		MinContribution:       minContributionPtr,
		MaxContribution:       maxContributionPtr,
		DeletedAt:             deletedAtPtr,
		AutoCompound:          autoCompound,
		YieldBalance:          yieldBalance,
	}, nil
}

func nullSQLString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// nullDecimal renders an optional decimal (e.g. a goal's per-contribution
// limit, #922) as a nullable SQL parameter so a nil pointer is persisted as
// NULL rather than "0".
func nullDecimal(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}
	return d.String()
}

// nullUUID renders an optional vault link as a nullable SQL parameter so a nil
// pointer is persisted as NULL rather than the zero UUID.
func nullUUID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return id.String()
}

// RecordGoalDeposits atomically inserts all per-goal deposit rows (#719).
func (r *SavingsGoalRepository) RecordGoalDeposits(ctx context.Context, deposits []savingsgoal.GoalDeposit) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO savings_goal_deposits (id, goal_id, user_id, amount, currency)
		VALUES ($1, $2, $3, $4::numeric, $5)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, d := range deposits {
		if _, err := stmt.ExecContext(ctx, d.ID, d.GoalID, d.UserID, d.Amount.String(), d.Currency); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SumGoalDeposits returns the total deposited to a specific goal via deposit-split (#719).
func (r *SavingsGoalRepository) SumGoalDeposits(ctx context.Context, goalID uuid.UUID) (decimal.Decimal, error) {
	var total sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount), 0) FROM savings_goal_deposits WHERE goal_id = $1`,
		goalID,
	).Scan(&total)
	if err != nil {
		return decimal.Zero, err
	}
	if !total.Valid || total.String == "" {
		return decimal.Zero, nil
	}
	return decimal.NewFromString(total.String)
}

func encodeGoalContributionCursor(createdAt time.Time, contributionID uuid.UUID) string {
	payload := createdAt.UTC().Format(time.RFC3339Nano) + "|" + contributionID.String()
	return base64.RawStdEncoding.EncodeToString([]byte(payload))
}

func decodeGoalContributionCursor(cursor string) (time.Time, uuid.UUID, error) {
	decoded, err := base64.RawStdEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, fmt.Errorf("invalid contribution cursor")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	contributionID, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	return createdAt, contributionID, nil
}
