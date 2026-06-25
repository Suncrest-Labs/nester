package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type mockSavingsGoalService struct {
	goals map[uuid.UUID]savingsgoal.SavingsGoal
}

func (m *mockSavingsGoalService) Create(_ context.Context, userID uuid.UUID, in service.CreateSavingsGoalInput) (savingsgoal.SavingsGoal, error) {
	if !savingsgoal.IsSupportedCurrency(in.Currency) {
		return savingsgoal.SavingsGoal{}, fmt.Errorf("%w: unsupported currency %q (supported: USDC, XLM)", savingsgoal.ErrInvalidGoal, in.Currency)
	}
	category := savingsgoal.CategoryOther
	if in.Category != "" {
		parsed, err := savingsgoal.ParseCategory(in.Category)
		if err != nil {
			return savingsgoal.SavingsGoal{}, err
		}
		category = parsed
	}
	g := savingsgoal.SavingsGoal{
		ID:            uuid.New(),
		UserID:        userID,
		TargetAmount:  in.TargetAmount,
		Currency:      savingsgoal.NormalizeCurrency(in.Currency),
		Deadline:      in.Deadline,
		Description:   in.Description,
		Category:      category,
		CurrentAmount: decimal.NewFromInt(100),
		ProgressPct:   10,
	}
	m.goals[g.ID] = g
	return g, nil
}

func (m *mockSavingsGoalService) Get(_ context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	g, ok := m.goals[goalID]
	if !ok || g.UserID != userID {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotFound
	}
	return g, nil
}

func (m *mockSavingsGoalService) List(_ context.Context, userID uuid.UUID, category string) ([]savingsgoal.SavingsGoal, error) {
	if category != "" {
		if _, err := savingsgoal.ParseCategory(category); err != nil {
			return nil, err
		}
	}
	var out []savingsgoal.SavingsGoal
	for _, g := range m.goals {
		if g.UserID != userID {
			continue
		}
		if category != "" && string(g.Category) != category {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}

func (m *mockSavingsGoalService) Update(_ context.Context, userID, goalID uuid.UUID, in service.UpdateSavingsGoalInput) (savingsgoal.SavingsGoal, error) {
	g, ok := m.goals[goalID]
	if !ok || g.UserID != userID {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotFound
	}
	if in.TargetAmount != nil {
		g.TargetAmount = *in.TargetAmount
	}
	if in.Category != nil {
		parsed, err := savingsgoal.ParseCategory(*in.Category)
		if err != nil {
			return savingsgoal.SavingsGoal{}, err
		}
		g.Category = parsed
	}
	m.goals[goalID] = g
	return g, nil
}

func (m *mockSavingsGoalService) Delete(_ context.Context, userID, goalID uuid.UUID) error {
	g, ok := m.goals[goalID]
	if !ok || g.UserID != userID {
		return savingsgoal.ErrGoalNotFound
	}
	delete(m.goals, goalID)
	return nil
}

func (m *mockSavingsGoalService) Summary(_ context.Context, userID uuid.UUID) (savingsgoal.SavingsGoalsSummary, error) {
	summary := savingsgoal.SavingsGoalsSummary{}
	for _, g := range m.goals {
		if g.UserID != userID {
			continue
		}
		summary.GoalCount++
		switch g.Currency {
		case savingsgoal.CurrencyUSDC:
			summary.TotalTargetUSDC = summary.TotalTargetUSDC.Add(g.TargetAmount)
			summary.TotalSavedUSDC = g.CurrentAmount
		case savingsgoal.CurrencyXLM:
			summary.TotalTargetXLM = summary.TotalTargetXLM.Add(g.TargetAmount)
			summary.TotalSavedXLM = g.CurrentAmount
		}
	}
	return summary, nil
}

func withAuthUser(next http.Handler, userID uuid.UUID) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := auth.User{ID: userID.String(), WalletAddress: "GTEST"}
		next.ServeHTTP(w, r.WithContext(auth.NewContext(r.Context(), u)))
	})
}

func TestSavingsGoalHandler_CRUD(t *testing.T) {
	userID := uuid.New()
	svc := &mockSavingsGoalService{goals: make(map[uuid.UUID]savingsgoal.SavingsGoal)}
	h := NewSavingsGoalHandler(svc)

	mux := http.NewServeMux()
	h.Register(mux)
	handler := middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(
		withAuthUser(mux, userID),
	)
	server := httptest.NewServer(handler)
	defer server.Close()

	deadline := time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	createBody := `{"target_amount":1000,"currency":"USDC","deadline":"` + deadline + `","description":"Emergency fund"}`
	resp, err := http.Post(server.URL+"/api/v1/users/savings-goals", "application/json", bytes.NewBufferString(createBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}

	listResp, err := http.Get(server.URL + "/api/v1/users/savings-goals")
	if err != nil {
		t.Fatal(err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listResp.StatusCode)
	}
}

func TestSavingsGoalHandler_Create_InvalidCategory(t *testing.T) {
	userID := uuid.New()
	h := NewSavingsGoalHandler(&mockSavingsGoalService{goals: make(map[uuid.UUID]savingsgoal.SavingsGoal)})

	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(withAuthUser(mux, userID))
	defer server.Close()

	deadline := time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	createBody := `{"target_amount":1000,"currency":"USDC","deadline":"` + deadline + `","category":"vacation"}`
	resp, err := http.Post(server.URL+"/api/v1/users/savings-goals", "application/json", bytes.NewBufferString(createBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("create status = %d, want 400", resp.StatusCode)
	}
}

func TestSavingsGoalHandler_Create_DefaultCategory(t *testing.T) {
	userID := uuid.New()
	h := NewSavingsGoalHandler(&mockSavingsGoalService{goals: make(map[uuid.UUID]savingsgoal.SavingsGoal)})

	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(withAuthUser(mux, userID))
	defer server.Close()

	deadline := time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	createBody := `{"target_amount":1000,"currency":"USDC","deadline":"` + deadline + `"}`
	resp, err := http.Post(server.URL+"/api/v1/users/savings-goals", "application/json", bytes.NewBufferString(createBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}

	var envelope response.Response
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatal(err)
	}
	var goal savingsgoal.SavingsGoal
	if err := json.Unmarshal(data, &goal); err != nil {
		t.Fatal(err)
	}
	if goal.Category != savingsgoal.CategoryOther {
		t.Fatalf("category = %q, want other", goal.Category)
	}
}

func TestSavingsGoalHandler_List_FilterByCategory(t *testing.T) {
	userID := uuid.New()
	goalID := uuid.New()
	svc := &mockSavingsGoalService{goals: map[uuid.UUID]savingsgoal.SavingsGoal{
		goalID: {
			ID:           goalID,
			UserID:       userID,
			TargetAmount: decimal.NewFromInt(1000),
			Currency:     "USDC",
			Category:     savingsgoal.CategoryEducation,
		},
	}}
	h := NewSavingsGoalHandler(svc)

	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(withAuthUser(mux, userID))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/users/savings-goals?category=education")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", resp.StatusCode)
	}

	var envelope response.Response
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatal(err)
	}
	var goals []savingsgoal.SavingsGoal
	if err := json.Unmarshal(data, &goals); err != nil {
		t.Fatal(err)
	}
	if len(goals) != 1 || goals[0].Category != savingsgoal.CategoryEducation {
		t.Fatalf("goals = %+v, want one education goal", goals)
	}
}

func TestSavingsGoalHandler_Create_ValidXLM(t *testing.T) {
	userID := uuid.New()
	h := NewSavingsGoalHandler(&mockSavingsGoalService{goals: make(map[uuid.UUID]savingsgoal.SavingsGoal)})

	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(withAuthUser(mux, userID))
	defer server.Close()

	deadline := time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	createBody := `{"target_amount":500,"currency":"XLM","deadline":"` + deadline + `","description":"Staking"}`
	resp, err := http.Post(server.URL+"/api/v1/users/savings-goals", "application/json", bytes.NewBufferString(createBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
}

func TestSavingsGoalHandler_Create_InvalidCurrency(t *testing.T) {
	userID := uuid.New()
	h := NewSavingsGoalHandler(&mockSavingsGoalService{goals: make(map[uuid.UUID]savingsgoal.SavingsGoal)})

	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(withAuthUser(mux, userID))
	defer server.Close()

	deadline := time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	createBody := `{"target_amount":1000,"currency":"NGN","deadline":"` + deadline + `"}`
	resp, err := http.Post(server.URL+"/api/v1/users/savings-goals", "application/json", bytes.NewBufferString(createBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("create status = %d, want 400", resp.StatusCode)
	}
}
