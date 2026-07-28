package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/intelligence"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

type fakeGoalCoachingRepo struct {
	goals []savingsgoal.SavingsGoal
	err   error
}

func (f fakeGoalCoachingRepo) ListActiveApproachingDeadline(_ context.Context, _ int) ([]savingsgoal.SavingsGoal, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.goals, nil
}

type fakeGoalCoachingClient struct {
	resp *intelligence.CoachingResponse
	err  error
}

func (f fakeGoalCoachingClient) GetGoalCoaching(_ context.Context, _ intelligence.CoachingRequest) (*intelligence.CoachingResponse, error) {
	return f.resp, f.err
}

type fakePreferenceStore struct{}

func (fakePreferenceStore) Get(_ context.Context, _ uuid.UUID) (notifications.Preferences, error) {
	return notifications.DefaultPreferences(), nil
}

// fakeNudgePreferenceChecker implements nudge.PreferenceChecker for tests.
type fakeNudgePreferenceChecker struct {
	enabled bool
	err     error
}

func (f fakeNudgePreferenceChecker) NudgesEnabled(_ context.Context, _ uuid.UUID) (bool, error) {
	return f.enabled, f.err
}

type recordingChannel struct {
	mu        sync.Mutex
	delivered []notifications.Notification
}

func (c *recordingChannel) Kind() notifications.ChannelKind { return notifications.ChannelPush }

func (c *recordingChannel) Deliver(_ context.Context, n notifications.Notification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.delivered = append(c.delivered, n)
	return nil
}

func (c *recordingChannel) all() []notifications.Notification {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]notifications.Notification, len(c.delivered))
	copy(out, c.delivered)
	return out
}

func TestGoalCoachingScheduler_TickSendsOnePerActiveGoal(t *testing.T) {
	userID := uuid.New()
	goal := savingsgoal.SavingsGoal{
		ID:            uuid.New(),
		UserID:        userID,
		TargetAmount:  decimal.NewFromInt(1000),
		Currency:      "USDC",
		Deadline:      time.Now().Add(90 * 24 * time.Hour),
		CurrentAmount: decimal.NewFromInt(340),
		ProgressPct:   34,
	}

	channel := &recordingChannel{}
	dispatcher := notifications.New([]notifications.Channel{channel}, fakePreferenceStore{}, nil)

	scheduler := NewGoalCoachingScheduler(
		fakeGoalCoachingRepo{goals: []savingsgoal.SavingsGoal{goal}},
		fakeGoalCoachingClient{resp: &intelligence.CoachingResponse{
			ProgressAssessment: "You're 34% toward your goal.",
			Nudges:             []string{"Keep going!"},
		}},
		dispatcher,
		nil,
		fakeNudgePreferenceChecker{enabled: true},
	)

	scheduler.tick(t.Context())

	delivered := channel.all()
	if len(delivered) != 1 {
		t.Fatalf("expected 1 delivered notification, got %d", len(delivered))
	}
	if delivered[0].UserID != userID {
		t.Errorf("UserID = %s, want %s", delivered[0].UserID, userID)
	}
	if delivered[0].Type != notifications.EventGoalCoaching {
		t.Errorf("Type = %s, want %s", delivered[0].Type, notifications.EventGoalCoaching)
	}
	if delivered[0].Body != "You're 34% toward your goal." {
		t.Errorf("Body = %q", delivered[0].Body)
	}
	if delivered[0].Payload["goal_id"] != goal.ID.String() {
		t.Errorf("Payload[goal_id] = %v, want %s", delivered[0].Payload["goal_id"], goal.ID.String())
	}
}

func TestGoalCoachingScheduler_TickSkipsGoalOnCoachingError(t *testing.T) {
	userID := uuid.New()
	goal := savingsgoal.SavingsGoal{ID: uuid.New(), UserID: userID, Currency: "USDC"}

	channel := &recordingChannel{}
	dispatcher := notifications.New([]notifications.Channel{channel}, fakePreferenceStore{}, nil)

	scheduler := NewGoalCoachingScheduler(
		fakeGoalCoachingRepo{goals: []savingsgoal.SavingsGoal{goal}},
		fakeGoalCoachingClient{err: context.DeadlineExceeded},
		dispatcher,
		nil,
		fakeNudgePreferenceChecker{enabled: true},
	)

	scheduler.tick(t.Context())

	if len(channel.all()) != 0 {
		t.Fatalf("expected no notifications delivered when coaching fails, got %d", len(channel.all()))
	}
}

func TestGoalCoachingScheduler_TickNoopWithoutDependencies(t *testing.T) {
	scheduler := NewGoalCoachingScheduler(nil, nil, nil, nil, nil)
	scheduler.tick(t.Context()) // must not panic
}

func TestGoalCoachingScheduler_SkipsGoalWhenUserOptedOut(t *testing.T) {
	userID := uuid.New()
	goal := savingsgoal.SavingsGoal{ID: uuid.New(), UserID: userID, Currency: "USDC"}

	channel := &recordingChannel{}
	dispatcher := notifications.New([]notifications.Channel{channel}, fakePreferenceStore{}, nil)
	client := &fakeGoalCoachingClient{resp: &intelligence.CoachingResponse{ProgressAssessment: "should not be sent"}}

	scheduler := NewGoalCoachingScheduler(
		fakeGoalCoachingRepo{goals: []savingsgoal.SavingsGoal{goal}},
		client,
		dispatcher,
		nil,
		fakeNudgePreferenceChecker{enabled: false},
	)

	scheduler.tick(t.Context())

	if len(channel.all()) != 0 {
		t.Fatalf("expected no notifications delivered for an opted-out user, got %d", len(channel.all()))
	}
}

func TestGoalCoachingScheduler_PassesOptOutFlagThroughToIntelligenceRequest(t *testing.T) {
	userID := uuid.New()
	goal := savingsgoal.SavingsGoal{ID: uuid.New(), UserID: userID, Currency: "USDC"}

	channel := &recordingChannel{}
	dispatcher := notifications.New([]notifications.Channel{channel}, fakePreferenceStore{}, nil)
	client := &recordingGoalCoachingClient{
		resp: &intelligence.CoachingResponse{ProgressAssessment: "hi"},
	}

	scheduler := NewGoalCoachingScheduler(
		fakeGoalCoachingRepo{goals: []savingsgoal.SavingsGoal{goal}},
		client,
		dispatcher,
		nil,
		fakeNudgePreferenceChecker{enabled: true},
	)

	scheduler.tick(t.Context())

	if len(client.requests) != 1 {
		t.Fatalf("expected 1 coaching request, got %d", len(client.requests))
	}
	req := client.requests[0]
	if req.AIInsightsEnabled == nil || !*req.AIInsightsEnabled {
		t.Errorf("AIInsightsEnabled = %v, want pointer to true", req.AIInsightsEnabled)
	}
}

// recordingGoalCoachingClient captures every request it's called with, so a
// test can assert on the fields sent to the intelligence service (not just
// the response side-effects).
type recordingGoalCoachingClient struct {
	resp     *intelligence.CoachingResponse
	err      error
	requests []intelligence.CoachingRequest
}

func (f *recordingGoalCoachingClient) GetGoalCoaching(_ context.Context, req intelligence.CoachingRequest) (*intelligence.CoachingResponse, error) {
	f.requests = append(f.requests, req)
	return f.resp, f.err
}
