package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// inMemGoalRepo is a minimal savingsgoal.Repository for exercising the real
// SavingsGoalService end-to-end through the handler. (The service package's own
// *_test.go does not compile on main, so these integration-style tests for the
// auto-complete behaviour (#684) live in the handler package.)
type inMemGoalRepo struct {
	goals    map[uuid.UUID]savingsgoal.SavingsGoal
	balances map[string]decimal.Decimal
}

func newInMemGoalRepo() *inMemGoalRepo {
	return &inMemGoalRepo{
		goals:    make(map[uuid.UUID]savingsgoal.SavingsGoal),
		balances: make(map[string]decimal.Decimal),
	}
}

func (r *inMemGoalRepo) balKey(userID uuid.UUID, currency string) string {
	return userID.String() + ":" + savingsgoal.NormalizeCurrency(currency)
}

func (r *inMemGoalRepo) setBalance(userID uuid.UUID, currency string, amount decimal.Decimal) {
	r.balances[r.balKey(userID, currency)] = amount
}

func (r *inMemGoalRepo) Create(_ context.Context, goal *savingsgoal.SavingsGoal) error {
	now := time.Now().UTC()
	goal.CreatedAt = now
	goal.UpdatedAt = now
	r.goals[goal.ID] = *goal
	return nil
}

func (r *inMemGoalRepo) ListByUser(_ context.Context, userID uuid.UUID, category string) ([]savingsgoal.SavingsGoal, error) {
	var out []savingsgoal.SavingsGoal
	for _, g := range r.goals {
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

func (r *inMemGoalRepo) GetByID(_ context.Context, id uuid.UUID) (*savingsgoal.SavingsGoal, error) {
	g, ok := r.goals[id]
	if !ok {
		return nil, savingsgoal.ErrGoalNotFound
	}
	return &g, nil
}

func (r *inMemGoalRepo) Update(_ context.Context, goal *savingsgoal.SavingsGoal) error {
	if _, ok := r.goals[goal.ID]; !ok {
		return savingsgoal.ErrGoalNotFound
	}
	r.goals[goal.ID] = *goal
	return nil
}

func (r *inMemGoalRepo) Delete(_ context.Context, id, userID uuid.UUID) error {
	delete(r.goals, id)
	return nil
}

func (r *inMemGoalRepo) SumVaultBalance(_ context.Context, userID uuid.UUID, currency string) (decimal.Decimal, error) {
	if bal, ok := r.balances[r.balKey(userID, currency)]; ok {
		return bal, nil
	}
	return decimal.Zero, nil
}

func (r *inMemGoalRepo) UpdateMilestones(_ context.Context, goalID uuid.UUID, milestones []int) error {
	g, ok := r.goals[goalID]
	if !ok {
		return savingsgoal.ErrGoalNotFound
	}
	g.NotifiedMilestones = append([]int(nil), milestones...)
	r.goals[goalID] = g
	return nil
}

func realGoalServer(t *testing.T, repo *inMemGoalRepo, userID uuid.UUID) *httptest.Server {
	t.Helper()
	svc := service.NewSavingsGoalService(repo, nil)
	mux := http.NewServeMux()
	NewSavingsGoalHandler(svc).Register(mux)
	server := httptest.NewServer(withAuthUser(mux, userID))
	t.Cleanup(server.Close)
	return server
}

func createGoal(t *testing.T, server *httptest.Server, target int) savingsgoal.SavingsGoal {
	t.Helper()
	deadline := time.Now().UTC().Add(60 * 24 * time.Hour).Format(time.RFC3339)
	body := fmt.Sprintf(`{"target_amount":%d,"currency":"USDC","deadline":%q}`, target, deadline)
	resp, err := http.Post(server.URL+"/api/v1/users/savings-goals", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	return decodeGoal(t, resp)
}

func decodeGoal(t *testing.T, resp *http.Response) savingsgoal.SavingsGoal {
	t.Helper()
	var env response.Response
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(env.Data)
	var goal savingsgoal.SavingsGoal
	if err := json.Unmarshal(raw, &goal); err != nil {
		t.Fatal(err)
	}
	return goal
}

func TestSavingsGoal_StaysActiveBelowTarget(t *testing.T) {
	repo := newInMemGoalRepo()
	userID := uuid.New()
	repo.setBalance(userID, "USDC", decimal.NewFromInt(990)) // 99% of 1000
	server := realGoalServer(t, repo, userID)

	goal := createGoal(t, server, 1000)
	if goal.Status != savingsgoal.GoalStatusActive {
		t.Fatalf("status = %q, want active (99%%)", goal.Status)
	}
	if goal.CompletedAt != nil {
		t.Fatalf("completed_at = %v, want nil", goal.CompletedAt)
	}
}

func TestSavingsGoal_AutoCompletesAtTarget(t *testing.T) {
	repo := newInMemGoalRepo()
	userID := uuid.New()
	repo.setBalance(userID, "USDC", decimal.NewFromInt(1000)) // 100% of 1000
	server := realGoalServer(t, repo, userID)

	goal := createGoal(t, server, 1000)
	if goal.Status != savingsgoal.GoalStatusCompleted {
		t.Fatalf("status = %q, want completed (100%%)", goal.Status)
	}
	if goal.CompletedAt == nil {
		t.Fatal("completed_at = nil, want a timestamp")
	}
}

func TestSavingsGoal_ListFilterByStatus(t *testing.T) {
	repo := newInMemGoalRepo()
	userID := uuid.New()
	repo.setBalance(userID, "USDC", decimal.NewFromInt(50)) // both goals below target
	server := realGoalServer(t, repo, userID)

	// One active goal.
	createGoal(t, server, 1000)
	// One that we drive to completion by raising the balance, then re-reading.
	completed := createGoal(t, server, 40) // 50 >= 40 → completes on create
	if completed.Status != savingsgoal.GoalStatusCompleted {
		t.Fatalf("expected the small-target goal to complete, got %q", completed.Status)
	}

	resp, err := http.Get(server.URL + "/api/v1/users/savings-goals?status=completed")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var env response.Response
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(env.Data)
	var goals []savingsgoal.SavingsGoal
	if err := json.Unmarshal(raw, &goals); err != nil {
		t.Fatal(err)
	}
	if len(goals) != 1 || goals[0].Status != savingsgoal.GoalStatusCompleted {
		t.Fatalf("status=completed returned %+v, want exactly 1 completed goal", goals)
	}
}

func TestSavingsGoal_PatchCompletedReturns409(t *testing.T) {
	repo := newInMemGoalRepo()
	userID := uuid.New()
	repo.setBalance(userID, "USDC", decimal.NewFromInt(500))
	server := realGoalServer(t, repo, userID)

	goal := createGoal(t, server, 500) // completes on create
	if goal.Status != savingsgoal.GoalStatusCompleted {
		t.Fatalf("precondition: goal not completed (%q)", goal.Status)
	}

	req, _ := http.NewRequest(
		http.MethodPatch,
		server.URL+"/api/v1/users/savings-goals/"+goal.ID.String(),
		bytes.NewBufferString(`{"target_amount":2000}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("PATCH completed goal status = %d, want 409", resp.StatusCode)
	}
}
