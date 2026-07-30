package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/intelligence"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
)

type fakeCoachingProvider struct {
	resp *intelligence.CoachingResponse
	err  error
	got  intelligence.CoachingRequest
}

func (f *fakeCoachingProvider) GetGoalCoaching(_ context.Context, req intelligence.CoachingRequest) (*intelligence.CoachingResponse, error) {
	f.got = req
	return f.resp, f.err
}

func TestSavingsGoalHandler_Coaching_ReturnsAIAssessment(t *testing.T) {
	userID := uuid.New()
	goalID := uuid.New()
	svc := &mockSavingsGoalService{goals: map[uuid.UUID]savingsgoal.SavingsGoal{
		goalID: {
			ID:            goalID,
			UserID:        userID,
			TargetAmount:  decimal.NewFromInt(1000),
			Currency:      "USDC",
			Deadline:      time.Now().Add(60 * 24 * time.Hour),
			CurrentAmount: decimal.NewFromInt(340),
			ProgressPct:   34,
		},
	}}
	provider := &fakeCoachingProvider{resp: &intelligence.CoachingResponse{
		ProgressAssessment: "You're 34% toward your goal.",
		Nudges:             []string{"Keep going!"},
		Confidence:         "high",
	}}

	h := NewSavingsGoalHandler(svc, nil)
	h.SetCoachingProvider(provider)

	mux := http.NewServeMux()
	h.Register(mux)
	handler := middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(
		withAuthUser(mux, userID),
	)
	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/users/savings-goals/" + goalID.String() + "/coaching")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Data intelligence.CoachingResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.ProgressAssessment != "You're 34% toward your goal." {
		t.Errorf("ProgressAssessment = %q", body.Data.ProgressAssessment)
	}
	if provider.got.Goal.Currency != "USDC" || provider.got.Goal.ProgressPct != 34 {
		t.Errorf("coaching request not built from goal correctly: %+v", provider.got.Goal)
	}
}

func TestSavingsGoalHandler_Coaching_UnconfiguredReturns503(t *testing.T) {
	userID := uuid.New()
	goalID := uuid.New()
	svc := &mockSavingsGoalService{goals: map[uuid.UUID]savingsgoal.SavingsGoal{
		goalID: {ID: goalID, UserID: userID},
	}}
	h := NewSavingsGoalHandler(svc, nil) // no coaching provider set

	mux := http.NewServeMux()
	h.Register(mux)
	handler := withAuthUser(mux, userID)
	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/users/savings-goals/" + goalID.String() + "/coaching")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestSavingsGoalHandler_Coaching_NotFoundForOtherUsersGoal(t *testing.T) {
	userID := uuid.New()
	otherUserID := uuid.New()
	goalID := uuid.New()
	svc := &mockSavingsGoalService{goals: map[uuid.UUID]savingsgoal.SavingsGoal{
		goalID: {ID: goalID, UserID: otherUserID},
	}}
	h := NewSavingsGoalHandler(svc, nil)
	h.SetCoachingProvider(&fakeCoachingProvider{resp: &intelligence.CoachingResponse{}})

	mux := http.NewServeMux()
	h.Register(mux)
	handler := withAuthUser(mux, userID)
	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/users/savings-goals/" + goalID.String() + "/coaching")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
