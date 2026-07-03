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
		SELECT id, name, description, category, suggested_amount, currency, suggested_months, icon
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
		var t savingsgoal.GoalTemplate
		var amountStr string
		var category string
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &category, &amountStr, &t.Currency, &t.SuggestedMonths, &t.Icon); err != nil {
			return nil, err
		}
		t.Category = savingsgoal.GoalCategory(category)
		t.SuggestedAmount, _ = decimal.NewFromString(amountStr)
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

func (r *GoalTemplateRepository) GetByID(ctx context.Context, id uuid.UUID) (*savingsgoal.GoalTemplate, error) {
	query := `
		SELECT id, name, description, category, suggested_amount, currency, suggested_months, icon
		FROM goal_templates
		WHERE id = $1
	`
	var t savingsgoal.GoalTemplate
	var amountStr string
	var category string
	if err := r.db.QueryRowContext(ctx, query, id).Scan(&t.ID, &t.Name, &t.Description, &category, &amountStr, &t.Currency, &t.SuggestedMonths, &t.Icon); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, savingsgoal.ErrGoalNotFound
		}
		return nil, err
	}
	t.Category = savingsgoal.GoalCategory(category)
	t.SuggestedAmount, _ = decimal.NewFromString(amountStr)
	return &t, nil
}
