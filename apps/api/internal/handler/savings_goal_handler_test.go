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

func (m *mockSavingsGoalService) List(_ context.Context, userID uuid.UUID, category, status string, includeArchived bool) ([]savingsgoal.SavingsGoal, error) {
	if category != "" {
		if _, err := savingsgoal.ParseCategory(category); err != nil {
			return nil, err
		}
	}
	filterStatus, err := savingsgoal.ParseStatusFilter(status)
	if err != nil {
		return nil, err
	}
	var out []savingsgoal.SavingsGoal
	for _, g := range m.goals {
		if g.UserID != userID {
			continue
		}
		if category != "" && string(g.Category) != category {
			continue
		}
		if filterStatus != "" {
			if g.Status != filterStatus {
				continue
			}
		} else if !includeArchived && g.Status == savingsgoal.GoalStatusArchived {
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
func (m *mockSavingsGoalService) Complete(_ context.Context, userID, goalID uuid.UUID, action string) (savingsgoal.SavingsGoal, error) {
	g, ok := m.goals[goalID]
	if !ok || g.UserID != userID {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotFound
	}
	return g, nil
}
func (m *mockSavingsGoalService) Pause(_ context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	g, ok := m.goals[goalID]
	if !ok || g.UserID != userID {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotFound
	}
	return g, nil
}
func (m *mockSavingsGoalService) Resume(_ context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	g, ok := m.goals[goalID]
	if !ok || g.UserID != userID {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotFound
	}
	return g, nil
}
func (m *mockSavingsGoalService) Archive(_ context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	g, ok := m.goals[goalID]
	if !ok || g.UserID != userID {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotFound
	}
	g.Status = savingsgoal.GoalStatusArchived
	m.goals[goalID] = g
	return g, nil
}

func (m *mockSavingsGoalService) Unarchive(_ context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	g, ok := m.goals[goalID]
	if !ok || g.UserID != userID {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotFound
	}
	g.Status = savingsgoal.GoalStatusActive
	m.goals[goalID] = g
	return g, nil
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

func TestSavingsGoalHandler_List_FilterByStatus(t *testing.T) {
	userID := uuid.New()
	activeID, completedID := uuid.New(), uuid.New()
	svc := &mockSavingsGoalService{goals: map[uuid.UUID]savingsgoal.SavingsGoal{
		activeID: {ID: activeID, UserID: userID, TargetAmount: decimal.NewFromInt(1000),
			Currency: "USDC", Category: savingsgoal.CategoryEducation, Status: savingsgoal.GoalStatusActive},
		completedID: {ID: completedID, UserID: userID, TargetAmount: decimal.NewFromInt(500),
			Currency: "USDC", Category: savingsgoal.CategoryTravel, Status: savingsgoal.GoalStatusCompleted},
	}}
	h := NewSavingsGoalHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(withAuthUser(mux, userID))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/users/savings-goals?status=completed")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", resp.StatusCode)
	}
	goals := decodeGoalList(t, resp)
	if len(goals) != 1 || goals[0].Status != savingsgoal.GoalStatusCompleted {
		t.Fatalf("goals = %+v, want one completed goal", goals)
	}

	// Invalid status → 400.
	bad, err := http.Get(server.URL + "/api/v1/users/savings-goals?status=bogus")
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid status = %d, want 400", bad.StatusCode)
	}
}

func TestSavingsGoalHandler_Archive(t *testing.T) {
	userID := uuid.New()
	goalID := uuid.New()
	svc := &mockSavingsGoalService{goals: map[uuid.UUID]savingsgoal.SavingsGoal{
		goalID: {ID: goalID, UserID: userID, TargetAmount: decimal.NewFromInt(500),
			Currency: "USDC", Category: savingsgoal.CategoryOther, Status: savingsgoal.GoalStatusCompleted},
	}}
	h := NewSavingsGoalHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(withAuthUser(mux, userID))
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPatch,
		server.URL+"/api/v1/users/savings-goals/"+goalID.String()+"/archive", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("archive status = %d, want 200", resp.StatusCode)
	}

	var envelope response.Response
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(envelope.Data)
	var goal savingsgoal.SavingsGoal
	if err := json.Unmarshal(data, &goal); err != nil {
		t.Fatal(err)
	}
	if goal.Status != savingsgoal.GoalStatusArchived {
		t.Fatalf("archived goal status = %q, want archived", goal.Status)
	}
}

func TestSavingsGoalHandler_Unarchive(t *testing.T) {
	userID := uuid.New()
	goalID := uuid.New()
	svc := &mockSavingsGoalService{goals: map[uuid.UUID]savingsgoal.SavingsGoal{
		goalID: {ID: goalID, UserID: userID, TargetAmount: decimal.NewFromInt(500),
			Currency: "USDC", Category: savingsgoal.CategoryOther, Status: savingsgoal.GoalStatusArchived},
	}}
	h := NewSavingsGoalHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(withAuthUser(mux, userID))
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPatch,
		server.URL+"/api/v1/users/savings-goals/"+goalID.String()+"/unarchive", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unarchive status = %d, want 200", resp.StatusCode)
	}

	var envelope response.Response
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(envelope.Data)
	var goal savingsgoal.SavingsGoal
	if err := json.Unmarshal(data, &goal); err != nil {
		t.Fatal(err)
	}
	if goal.Status != savingsgoal.GoalStatusActive {
		t.Fatalf("unarchived goal status = %q, want active", goal.Status)
	}
}

func TestSavingsGoalHandler_List_ExcludesArchivedByDefault(t *testing.T) {
	userID := uuid.New()
	activeID, archivedID := uuid.New(), uuid.New()
	svc := &mockSavingsGoalService{goals: map[uuid.UUID]savingsgoal.SavingsGoal{
		activeID: {ID: activeID, UserID: userID, TargetAmount: decimal.NewFromInt(1000),
			Currency: "USDC", Category: savingsgoal.CategoryEducation, Status: savingsgoal.GoalStatusActive},
		archivedID: {ID: archivedID, UserID: userID, TargetAmount: decimal.NewFromInt(500),
			Currency: "USDC", Category: savingsgoal.CategoryOther, Status: savingsgoal.GoalStatusArchived},
	}}
	h := NewSavingsGoalHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(withAuthUser(mux, userID))
	defer server.Close()

	// Default list must exclude archived.
	resp, err := http.Get(server.URL + "/api/v1/users/savings-goals")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	goals := decodeGoalList(t, resp)
	if len(goals) != 1 || goals[0].ID != activeID {
		t.Fatalf("default list = %+v, want only the active goal", goals)
	}

	// include_archived=true must return both.
	resp2, err := http.Get(server.URL + "/api/v1/users/savings-goals?include_archived=true")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	all := decodeGoalList(t, resp2)
	if len(all) != 2 {
		t.Fatalf("include_archived list = %d goals, want 2", len(all))
	}
}

// decodeGoalList decodes a list-of-goals response envelope.
func decodeGoalList(t *testing.T, resp *http.Response) []savingsgoal.SavingsGoal {
	t.Helper()
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
	return goals
}
