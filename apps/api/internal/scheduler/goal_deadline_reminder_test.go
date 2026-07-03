package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
	"github.com/suncrestlabs/nester/apps/api/internal/scheduler"
)

type mockGoalDeadlineRepo struct {
	Goals         []savingsgoal.SavingsGoal
	UpdatedGoalID uuid.UUID
	Reminders     []int
}

func (m *mockGoalDeadlineRepo) ListActiveApproachingDeadline(ctx context.Context, maxDays int) ([]savingsgoal.SavingsGoal, error) {
	return m.Goals, nil
}

func (m *mockGoalDeadlineRepo) UpdateDeadlineReminders(ctx context.Context, goalID uuid.UUID, reminders []int) error {
	m.UpdatedGoalID = goalID
	m.Reminders = reminders
	return nil
}

type mockGoalProgressResolver struct{}

func (m *mockGoalProgressResolver) EnrichProgress(ctx context.Context, goal savingsgoal.SavingsGoal) (savingsgoal.SavingsGoal, error) {
	// For tests, just return the goal passed in.
	return goal, nil
}

type mockDeadlineNotifier struct {
	Called bool
	Title  string
}

func (m *mockDeadlineNotifier) Send(ctx context.Context, userID uuid.UUID, evt notifications.EventType, title, body string, payload map[string]any) error {
	m.Called = true
	m.Title = title
	return nil
}

func TestGoalDeadlineReminderJob_Tick(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name       string
		goal       savingsgoal.SavingsGoal
		wantNotify bool
		wantTitle  string
		wantReminders []int
	}{
		{
			name: "7 days left",
			goal: savingsgoal.SavingsGoal{
				ID: uuid.New(),
				Deadline: now.Add(7 * 24 * time.Hour),
				ProgressPct: 50,
			},
			wantNotify: true,
			wantTitle: "7 days left!",
			wantReminders: []int{7},
		},
		{
			name: "3 days left",
			goal: savingsgoal.SavingsGoal{
				ID: uuid.New(),
				Deadline: now.Add(3 * 24 * time.Hour),
				ProgressPct: 50,
			},
			wantNotify: true,
			wantTitle: "3 days remaining",
			wantReminders: []int{3},
		},
		{
			name: "1 day left",
			goal: savingsgoal.SavingsGoal{
				ID: uuid.New(),
				Deadline: now.Add(23 * time.Hour),
				ProgressPct: 50,
			},
			wantNotify: true,
			wantTitle: "Last day!",
			wantReminders: []int{1},
		},
		{
			name: "already notified for 3 days, now 3 days left",
			goal: savingsgoal.SavingsGoal{
				ID: uuid.New(),
				Deadline: now.Add(3 * 24 * time.Hour),
				ProgressPct: 50,
				DeadlineRemindersSent: []int{7, 3},
			},
			wantNotify: false,
		},
		{
			name: "completed goal skipped",
			goal: savingsgoal.SavingsGoal{
				ID: uuid.New(),
				Deadline: now.Add(3 * 24 * time.Hour),
				ProgressPct: 100,
			},
			wantNotify: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockGoalDeadlineRepo{Goals: []savingsgoal.SavingsGoal{tt.goal}}
			resolver := &mockGoalProgressResolver{}
			notifier := &mockDeadlineNotifier{}

			job := scheduler.NewGoalDeadlineReminderJob(
				scheduler.GoalDeadlineReminderConfig{Enabled: true, Interval: time.Hour},
				repo,
				resolver,
				notifier,
				nil,
			)
			job.SetClock(func() time.Time { return now })
			job.Tick(context.Background())

			assert.Equal(t, tt.wantNotify, notifier.Called)
			if tt.wantNotify {
				assert.Equal(t, tt.wantTitle, notifier.Title)
				assert.Equal(t, tt.wantReminders, repo.Reminders)
			}
		})
	}
}
