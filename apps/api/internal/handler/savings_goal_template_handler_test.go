package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/handler"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/pkg/listquery"
)

type mockGoalTemplateService struct {
	Templates []savingsgoal.GoalTemplate
	Goals     []savingsgoal.SavingsGoal
}

func (m *mockGoalTemplateService) Create(ctx context.Context, userID uuid.UUID, in service.CreateSavingsGoalInput) (savingsgoal.SavingsGoal, error) {
	return savingsgoal.SavingsGoal{}, nil
}
func (m *mockGoalTemplateService) Get(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	return savingsgoal.SavingsGoal{}, nil
}
func (m *mockGoalTemplateService) List(ctx context.Context, userID uuid.UUID, category, status string, includeArchived bool) ([]savingsgoal.SavingsGoal, error) {
	return nil, nil
}
func (m *mockGoalTemplateService) ListPaginated(ctx context.Context, userID uuid.UUID, filter service.SavingsGoalListFilter) ([]savingsgoal.SavingsGoal, int, error) {
	return m.Goals, len(m.Goals), nil
}
func (m *mockGoalTemplateService) Update(ctx context.Context, userID, goalID uuid.UUID, in service.UpdateSavingsGoalInput) (savingsgoal.SavingsGoal, error) {
	return savingsgoal.SavingsGoal{}, nil
}
func (m *mockGoalTemplateService) Delete(ctx context.Context, userID, goalID uuid.UUID) error {
	return nil
}
func (m *mockGoalTemplateService) Restore(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	return savingsgoal.SavingsGoal{}, nil
}
func (m *mockGoalTemplateService) Summary(ctx context.Context, userID uuid.UUID) (savingsgoal.SavingsGoalsSummary, error) {
	return savingsgoal.SavingsGoalsSummary{}, nil
}
func (m *mockGoalTemplateService) Pause(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	return savingsgoal.SavingsGoal{}, nil
}
func (m *mockGoalTemplateService) Resume(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	return savingsgoal.SavingsGoal{}, nil
}
func (m *mockGoalTemplateService) Complete(ctx context.Context, userID, goalID uuid.UUID, action string) (savingsgoal.SavingsGoal, error) {
	return savingsgoal.SavingsGoal{}, nil
}
func (m *mockGoalTemplateService) Archive(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	return savingsgoal.SavingsGoal{}, nil
}
func (m *mockGoalTemplateService) Unarchive(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	return savingsgoal.SavingsGoal{}, nil
}
func (m *mockGoalTemplateService) DepositSplit(ctx context.Context, userID uuid.UUID, in service.DepositSplitInput) (service.SplitDepositResult, error) {
	return service.SplitDepositResult{}, nil
}
func (m *mockGoalTemplateService) Share(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	return savingsgoal.SavingsGoal{}, nil
}
func (m *mockGoalTemplateService) Unshare(ctx context.Context, userID, goalID uuid.UUID) error {
	return nil
}
func (m *mockGoalTemplateService) GetShared(ctx context.Context, token uuid.UUID) (savingsgoal.SharedGoalView, error) {
	return savingsgoal.SharedGoalView{}, nil
}
func (m *mockGoalTemplateService) ListContributions(ctx context.Context, userID, goalID uuid.UUID, params listquery.PageParams) ([]savingsgoal.GoalContribution, int, string, error) {
	return nil, 0, "", nil
}
func (m *mockGoalTemplateService) ListTemplates(ctx context.Context) ([]savingsgoal.GoalTemplate, error) {
	return m.Templates, nil
}
func (m *mockGoalTemplateService) CreateFromTemplate(ctx context.Context, userID uuid.UUID, in service.CreateFromTemplateInput) (savingsgoal.SavingsGoal, error) {
	var template *savingsgoal.GoalTemplate
	for _, t := range m.Templates {
		if t.ID == in.TemplateID {
			template = &t
			break
		}
	}
	if template == nil {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotFound
	}
	amount := template.SuggestedAmount
	if in.OverrideAmount != nil {
		amount = *in.OverrideAmount
	}
	months := template.SuggestedMonths
	if in.OverrideMonths != nil {
		months = *in.OverrideMonths
	}
	deadline := time.Now().UTC().AddDate(0, months, 0)

	goal := savingsgoal.SavingsGoal{
		ID:           uuid.New(),
		UserID:       userID,
		Name:         template.Name,
		Description:  template.Description,
		TargetAmount: amount,
		Currency:     template.Currency,
		Deadline:     deadline,
		Category:     template.Category,
	}
	m.Goals = append(m.Goals, goal)
	return goal, nil
}

func TestGoalTemplateHandler(t *testing.T) {
	userID := uuid.New()
	templateID := uuid.New()

	svc := &mockGoalTemplateService{
		Templates: []savingsgoal.GoalTemplate{
			{
				ID:              templateID,
				Name:            "Vacation",
				SuggestedAmount: decimal.NewFromInt(1500),
				Currency:        "USDC",
				SuggestedMonths: 12,
			},
		},
	}
	h := handler.NewSavingsGoalHandler(svc, nil)

	mux := http.NewServeMux()
	h.Register(mux)

	authedClient := func(req *http.Request) *http.Request {
		return req.WithContext(auth.NewContext(req.Context(), auth.User{ID: userID.String()}))
	}

	t.Run("list templates", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/savings-goal-templates", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, authedClient(req))

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "Vacation")
	})

	t.Run("create from template without overrides", func(t *testing.T) {
		body := map[string]any{"template_id": templateID.String()}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/users/savings-goals/from-template", bytes.NewReader(b))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, authedClient(req))

		assert.Equal(t, http.StatusCreated, rec.Code)
		assert.Contains(t, rec.Body.String(), "1500")
	})

	t.Run("create from template with overrides", func(t *testing.T) {
		body := map[string]any{
			"template_id":     templateID.String(),
			"override_amount": "5000",
			"override_months": 6,
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/users/savings-goals/from-template", bytes.NewReader(b))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, authedClient(req))

		assert.Equal(t, http.StatusCreated, rec.Code)
		assert.Contains(t, rec.Body.String(), "5000")
	})

	t.Run("invalid template returns 404", func(t *testing.T) {
		body := map[string]any{"template_id": uuid.New().String()}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/users/savings-goals/from-template", bytes.NewReader(b))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, authedClient(req))

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}
