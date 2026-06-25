package service

import (
	"testing"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
)

func TestMilestoneNotificationContent(t *testing.T) {
	title, body := milestoneNotificationContent(50, "Vacation Fund")
	if title != "Halfway there!" {
		t.Fatalf("title = %q", title)
	}
	if body != "You've hit the halfway mark on Vacation Fund. You're on track!" {
		t.Fatalf("body = %q", body)
	}

	title, body = milestoneNotificationContent(100, "Emergency Fund")
	if title != "Goal achieved! 🎉" {
		t.Fatalf("title = %q", title)
	}
	if body != "Congratulations! You've fully funded your Emergency Fund goal." {
		t.Fatalf("body = %q", body)
	}
}

func TestMilestoneNotificationContent_AllMilestonesDistinct(t *testing.T) {
	seen := make(map[string]struct{})
	for _, m := range savingsgoal.ProgressMilestones {
		title, body := milestoneNotificationContent(m, "Test Goal")
		key := title + "|" + body
		if _, ok := seen[key]; ok {
			t.Fatalf("duplicate content for milestone %d", m)
		}
		seen[key] = struct{}{}
		if title == "" || body == "" {
			t.Fatalf("milestone %d has empty title or body", m)
		}
	}
}

func TestDispatcherGoalMilestoneNotifier_NilDispatcherNoOp(t *testing.T) {
	n := DispatcherGoalMilestoneNotifier{}
	n.SendGoalMilestone(
		t.Context(),
		uuid.New(),
		savingsgoal.SavingsGoal{Description: "Test"},
		25,
	)
}
