package usersignal

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
)

type HeuristicSegmentProvider struct {
	UserRepo user.UserRepository
	GoalRepo savingsgoal.Repository
}

// DeriveSegment buckets a user along a state/value axis, distinct from the
// engagement-recency axis EngagementProvider computes: a user can be both
// "active_saver" and "at_risk" on engagement, or "high_value" regardless of
// recency. Recency only decides the dormant/at-risk end of this segment
// when there's no other signal to go on.
func (p HeuristicSegmentProvider) DeriveSegment(ctx context.Context, userID uuid.UUID) (Segment, error) {
	u, err := p.UserRepo.GetByID(ctx, userID)
	if err != nil {
		return SegmentNewUser, err
	}

	var goals []savingsgoal.SavingsGoal
	if p.GoalRepo != nil {
		goals, _ = p.GoalRepo.ListByUser(ctx, userID, "", "")
	}
	if len(goals) == 0 {
		return SegmentNewUser, nil
	}

	if u.Tier == "premium" || u.Tier == "vip" {
		return SegmentHighValue, nil
	}

	if u.LastLoginAt == nil {
		return SegmentDormant, nil
	}
	daysSinceLogin := time.Since(*u.LastLoginAt).Hours() / 24
	if daysSinceLogin > 30 {
		return SegmentDormant, nil
	}
	if daysSinceLogin > 14 {
		return SegmentAtRisk, nil
	}
	return SegmentActiveSaver, nil
}

type HeuristicEngagementProvider struct {
	UserRepo user.UserRepository
}

func (p HeuristicEngagementProvider) ComputeEngagement(ctx context.Context, userID uuid.UUID) (EngagementScore, EngagementTier, error) {
	u, err := p.UserRepo.GetByID(ctx, userID)
	if err != nil {
		return EngagementScore{Score: 0.5}, TierEngaged, err
	}

	if u.LastLoginAt == nil {
		return EngagementScore{Score: 0.2}, TierAtRisk, nil
	}

	daysSinceLogin := time.Since(*u.LastLoginAt).Hours() / 24
	if daysSinceLogin <= 7 {
		return EngagementScore{Score: 0.9}, TierHighlyEngaged, nil
	} else if daysSinceLogin <= 14 {
		return EngagementScore{Score: 0.7}, TierEngaged, nil
	} else if daysSinceLogin <= 30 {
		return EngagementScore{Score: 0.4}, TierAtRisk, nil
	}
	return EngagementScore{Score: 0.1}, TierDormant, nil
}

type HeuristicTimingProvider struct {
	Activity ActivityEventRepository
	UserRepo user.UserRepository
}

func (p HeuristicTimingProvider) InferResponsiveWindow(ctx context.Context, userID uuid.UUID) (ResponsiveWindow, error) {
	u, err := p.UserRepo.GetByID(ctx, userID)
	if err != nil {
		return ResponsiveWindow{HourOfDay: 12, Timezone: "UTC"}, err
	}
	events, err := p.Activity.ListRecentEvents(ctx, userID, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		return ResponsiveWindow{HourOfDay: 12, Timezone: u.Timezone}, err
	}
	return InferResponsiveWindow(events, u.Timezone), nil
}
