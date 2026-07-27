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
		INSERT INTO savings_goals (id, user_id, vault_id, target_amount, currency, deadline, description, category, name, emoji)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING created_at, updated_at, status
	`
	return r.db.QueryRowContext(
		ctx, query,
		goal.ID, goal.UserID, nullUUID(goal.VaultID), goal.TargetAmount.String(), goal.Currency, goal.Deadline,
		nullSQLString(goal.Description), string(goal.Category),
		nullSQLString(goal.Name), nullSQLString(goal.Emoji),
	).Scan(&goal.CreatedAt, &goal.UpdatedAt, &goal.Status)
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
		       share_token, share_enabled_at, onchain_goal_id, onchain_status
		FROM savings_goals WHERE share_token = $1
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

func (r *SavingsGoalRepository) ListByUser(ctx context.Context, userID uuid.UUID, category string) ([]savingsgoal.SavingsGoal, error) {
	query := `
		SELECT id, user_id, vault_id, target_amount, currency, deadline, description, category,
		       notified_milestones, deadline_reminders_sent, created_at, updated_at,
		       status, completed_at, completion_action, name, emoji,
		       share_token, share_enabled_at, onchain_goal_id, onchain_status
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
		       share_token, share_enabled_at, onchain_goal_id, onchain_status
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
		    vault_id = $6, name = $7, emoji = $8, updated_at = NOW()
		WHERE id = $9 AND user_id = $10
	`, goal.TargetAmount.String(), goal.Currency, goal.Deadline, nullSQLString(goal.Description),
		string(goal.Category), nullUUID(goal.VaultID), nullSQLString(goal.Name), nullSQLString(goal.Emoji),
		goal.ID, goal.UserID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsgoal.ErrGoalNotFound
	}
	return nil
}

// Delete soft-archives the goal instead of destroying the row (#685): it
// stamps archived_at and moves the goal to the archived status. Already
// archived goals are not matched, so a repeat DELETE surfaces as
// ErrGoalNotFound (404) and no row is ever permanently removed via this path.
func (r *SavingsGoalRepository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE savings_goals
		SET archived_at = NOW(), status = 'archived', updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND status <> 'archived'
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
		       share_token, share_enabled_at, onchain_goal_id, onchain_status
		FROM savings_goals
		WHERE (status = 'active' OR status IS NULL OR status = '')
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
	)
	if err := row.Scan(
		&id, &userID, &vaultID, &targetStr, &currency, &deadline, &description, &category,
		&notifiedMilestones, &deadlineReminders, &createdAt, &updatedAt,
		&status, &completedAt, &completionAction, &name, &emoji,
		&shareToken, &shareEnabledAt, &onchainGoalID, &onchainStatus,
	); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
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
	}, nil
}

func nullSQLString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
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
