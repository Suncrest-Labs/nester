package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
)

type GoalTemplateRepository struct {
	db *sql.DB
}

func NewGoalTemplateRepository(db *sql.DB) *GoalTemplateRepository {
	return &GoalTemplateRepository{db: db}
}

func (r *GoalTemplateRepository) List(ctx context.Context) ([]savingsgoal.GoalTemplate, error) {
	query := `
		SELECT id, name, description, category, suggested_amount, currency, suggested_months, icon,
		       is_custom, created_by, created_at, updated_at
		FROM goal_templates
		ORDER BY name ASC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []savingsgoal.GoalTemplate
	for rows.Next() {
		t, err := scanGoalTemplate(rows)
		if err != nil {
			return nil, err
		}
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

func (r *GoalTemplateRepository) GetByID(ctx context.Context, id uuid.UUID) (*savingsgoal.GoalTemplate, error) {
	query := `
		SELECT id, name, description, category, suggested_amount, currency, suggested_months, icon,
		       is_custom, created_by, created_at, updated_at
		FROM goal_templates
		WHERE id = $1
	`
	t, err := scanGoalTemplate(r.db.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, savingsgoal.ErrGoalNotFound
		}
		return nil, err
	}
	return &t, nil
}

// Create persists an admin-published template (#919), allowing the catalog
// to grow beyond the pre-built defaults seeded by migration 056 without a
// redeploy.
func (r *GoalTemplateRepository) Create(ctx context.Context, template *savingsgoal.GoalTemplate) error {
	if template.ID == uuid.Nil {
		template.ID = uuid.New()
	}
	query := `
		INSERT INTO goal_templates (id, name, description, category, suggested_amount, currency, suggested_months, icon, is_custom, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING created_at, updated_at
	`
	return r.db.QueryRowContext(
		ctx, query,
		template.ID, template.Name, template.Description, string(template.Category),
		template.SuggestedAmount.String(), template.Currency, template.SuggestedMonths, template.Icon,
		template.IsCustom, nullUUID(template.CreatedBy),
	).Scan(&template.CreatedAt, &template.UpdatedAt)
}

// Update modifies an existing template (#919), whether it was seeded as a
// pre-built default or published by an admin.
func (r *GoalTemplateRepository) Update(ctx context.Context, template *savingsgoal.GoalTemplate) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE goal_templates
		SET name = $1, description = $2, category = $3, suggested_amount = $4,
		    currency = $5, suggested_months = $6, icon = $7, updated_at = NOW()
		WHERE id = $8
	`, template.Name, template.Description, string(template.Category),
		template.SuggestedAmount.String(), template.Currency, template.SuggestedMonths, template.Icon,
		template.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsgoal.ErrGoalNotFound
	}
	return nil
}

// Delete removes a template from the catalog (#919).
func (r *GoalTemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM goal_templates WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return savingsgoal.ErrGoalNotFound
	}
	return nil
}

type goalTemplateScanner interface {
	Scan(dest ...any) error
}

func scanGoalTemplate(row goalTemplateScanner) (savingsgoal.GoalTemplate, error) {
	var t savingsgoal.GoalTemplate
	var amountStr, category string
	var createdBy sql.NullString
	var createdAt, updatedAt sql.NullTime
	if err := row.Scan(
		&t.ID, &t.Name, &t.Description, &category, &amountStr, &t.Currency, &t.SuggestedMonths, &t.Icon,
		&t.IsCustom, &createdBy, &createdAt, &updatedAt,
	); err != nil {
		return savingsgoal.GoalTemplate{}, err
	}
	t.Category = savingsgoal.GoalCategory(category)
	t.SuggestedAmount, _ = decimal.NewFromString(amountStr)
	if createdBy.Valid {
		if id, err := uuid.Parse(createdBy.String); err == nil {
			t.CreatedBy = &id
		}
	}
	if createdAt.Valid {
		t.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		t.UpdatedAt = updatedAt.Time
	}
	return t, nil
}
