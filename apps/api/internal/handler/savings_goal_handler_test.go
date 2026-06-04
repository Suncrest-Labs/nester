package handler

import (
	"bytes"
	"context"
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
)

type mockSavingsGoalService struct {
	goals map[uuid.UUID]savingsgoal.SavingsGoal
}

func (m *mockSavingsGoalService) Create(_ context.Context, userID uuid.UUID, in service.CreateSavingsGoalInput) (savingsgoal.SavingsGoal, error) {
	g := savingsgoal.SavingsGoal{
		ID:            uuid.New(),
		UserID:        userID,
		TargetAmount:  in.TargetAmount,
		Currency:      in.Currency,
		Deadline:      in.Deadline,
		Description:   in.Description,
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

func (m *mockSavingsGoalService) List(_ context.Context, userID uuid.UUID) ([]savingsgoal.SavingsGoal, error) {
	var out []savingsgoal.SavingsGoal
	for _, g := range m.goals {
		if g.UserID == userID {
			out = append(out, g)
		}
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
