package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
)

// CreateGoalTemplateInput is the admin input for publishing a new savings
// goal template (#919), extending the catalog beyond the pre-built defaults
// seeded by migration 056 without requiring a redeploy.
type CreateGoalTemplateInput struct {
	Name            string
	Description     string
	Category        string
	SuggestedAmount decimal.Decimal
	Currency        string
	SuggestedMonths int
	Icon            string
	CreatedBy       uuid.UUID
}

// UpdateGoalTemplateInput is the admin input for editing an existing
// template (#919). Nil fields are left unchanged.
type UpdateGoalTemplateInput struct {
	ID              uuid.UUID
	Name            *string
	Description     *string
	Category        *string
	SuggestedAmount *decimal.Decimal
	Currency        *string
	SuggestedMonths *int
	Icon            *string
}

// ListGoalTemplates returns the full catalog of savings goal templates,
// pre-built defaults and admin-published entries alike (#919).
func (s *AdminService) ListGoalTemplates(ctx context.Context) ([]savingsgoal.GoalTemplate, error) {
	if s.templateRepo == nil {
		return nil, fmt.Errorf("goal templates not configured")
	}
	return s.templateRepo.List(ctx)
}

// CreateGoalTemplate publishes a new curated template to the catalog (#919).
func (s *AdminService) CreateGoalTemplate(ctx context.Context, in CreateGoalTemplateInput) (savingsgoal.GoalTemplate, error) {
	if s.templateRepo == nil {
		return savingsgoal.GoalTemplate{}, fmt.Errorf("goal templates not configured")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return savingsgoal.GoalTemplate{}, fmt.Errorf("%w: name is required", ErrInvalidAdminInput)
	}
	description := strings.TrimSpace(in.Description)
	if description == "" {
		return savingsgoal.GoalTemplate{}, fmt.Errorf("%w: description is required", ErrInvalidAdminInput)
	}
	category, err := savingsgoal.ParseCategory(in.Category)
	if err != nil {
		return savingsgoal.GoalTemplate{}, fmt.Errorf("%w: %s", ErrInvalidAdminInput, err.Error())
	}
	if !in.SuggestedAmount.IsPositive() {
		return savingsgoal.GoalTemplate{}, fmt.Errorf("%w: suggested_amount must be greater than zero", ErrInvalidAdminInput)
	}
	currency := savingsgoal.NormalizeCurrency(in.Currency)
	if !savingsgoal.IsSupportedCurrency(currency) {
		return savingsgoal.GoalTemplate{}, fmt.Errorf("%w: unsupported currency %q", ErrInvalidAdminInput, in.Currency)
	}
	if in.SuggestedMonths <= 0 {
		return savingsgoal.GoalTemplate{}, fmt.Errorf("%w: suggested_months must be greater than zero", ErrInvalidAdminInput)
	}
	icon := strings.TrimSpace(in.Icon)
	if icon == "" {
		icon = savingsgoal.DefaultIconForCategory(category)
	}

	template := &savingsgoal.GoalTemplate{
		ID:              uuid.New(),
		Name:            name,
		Description:     description,
		Category:        category,
		SuggestedAmount: in.SuggestedAmount,
		Currency:        currency,
		SuggestedMonths: in.SuggestedMonths,
		Icon:            icon,
		IsCustom:        true,
	}
	if in.CreatedBy != uuid.Nil {
		createdBy := in.CreatedBy
		template.CreatedBy = &createdBy
	}
	if err := s.templateRepo.Create(ctx, template); err != nil {
		return savingsgoal.GoalTemplate{}, err
	}
	return *template, nil
}

// UpdateGoalTemplate edits an existing template (#919). Admins may edit
// pre-built defaults as well as their own custom templates, matching the
// rest of the admin-managed catalog data in this domain.
func (s *AdminService) UpdateGoalTemplate(ctx context.Context, in UpdateGoalTemplateInput) (savingsgoal.GoalTemplate, error) {
	if s.templateRepo == nil {
		return savingsgoal.GoalTemplate{}, fmt.Errorf("goal templates not configured")
	}
	existing, err := s.templateRepo.GetByID(ctx, in.ID)
	if err != nil {
		return savingsgoal.GoalTemplate{}, err
	}

	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return savingsgoal.GoalTemplate{}, fmt.Errorf("%w: name is required", ErrInvalidAdminInput)
		}
		existing.Name = name
	}
	if in.Description != nil {
		description := strings.TrimSpace(*in.Description)
		if description == "" {
			return savingsgoal.GoalTemplate{}, fmt.Errorf("%w: description is required", ErrInvalidAdminInput)
		}
		existing.Description = description
	}
	if in.Category != nil {
		category, err := savingsgoal.ParseCategory(*in.Category)
		if err != nil {
			return savingsgoal.GoalTemplate{}, fmt.Errorf("%w: %s", ErrInvalidAdminInput, err.Error())
		}
		existing.Category = category
	}
	if in.SuggestedAmount != nil {
		if !in.SuggestedAmount.IsPositive() {
			return savingsgoal.GoalTemplate{}, fmt.Errorf("%w: suggested_amount must be greater than zero", ErrInvalidAdminInput)
		}
		existing.SuggestedAmount = *in.SuggestedAmount
	}
	if in.Currency != nil {
		currency := savingsgoal.NormalizeCurrency(*in.Currency)
		if !savingsgoal.IsSupportedCurrency(currency) {
			return savingsgoal.GoalTemplate{}, fmt.Errorf("%w: unsupported currency %q", ErrInvalidAdminInput, *in.Currency)
		}
		existing.Currency = currency
	}
	if in.SuggestedMonths != nil {
		if *in.SuggestedMonths <= 0 {
			return savingsgoal.GoalTemplate{}, fmt.Errorf("%w: suggested_months must be greater than zero", ErrInvalidAdminInput)
		}
		existing.SuggestedMonths = *in.SuggestedMonths
	}
	if in.Icon != nil {
		icon := strings.TrimSpace(*in.Icon)
		if icon == "" {
			return savingsgoal.GoalTemplate{}, fmt.Errorf("%w: icon is required", ErrInvalidAdminInput)
		}
		existing.Icon = icon
	}

	if err := s.templateRepo.Update(ctx, existing); err != nil {
		return savingsgoal.GoalTemplate{}, err
	}
	return *existing, nil
}

// DeleteGoalTemplate removes a template from the catalog (#919).
func (s *AdminService) DeleteGoalTemplate(ctx context.Context, id uuid.UUID) error {
	if s.templateRepo == nil {
		return fmt.Errorf("goal templates not configured")
	}
	return s.templateRepo.Delete(ctx, id)
}
