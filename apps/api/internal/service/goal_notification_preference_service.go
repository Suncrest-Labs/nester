package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/goalnotification"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
)

// GoalNotificationPreferenceService manages per-goal mute/digest-frequency
// settings, so a user with many savings goals can quiet a specific goal's
// notifications or batch them instead of being notified on every milestone.
type GoalNotificationPreferenceService struct {
	repo     goalnotification.Repository
	goalRepo savingsgoal.Repository
}

func NewGoalNotificationPreferenceService(repo goalnotification.Repository, goalRepo savingsgoal.Repository) *GoalNotificationPreferenceService {
	return &GoalNotificationPreferenceService{repo: repo, goalRepo: goalRepo}
}

// Get returns the goal's stored notification preference, or the "immediate,
// unmuted" default if none has been set yet.
func (s *GoalNotificationPreferenceService) Get(ctx context.Context, userID, goalID uuid.UUID) (goalnotification.Preference, error) {
	if err := s.mustOwnGoal(ctx, userID, goalID); err != nil {
		return goalnotification.Preference{}, err
	}
	pref, err := s.repo.Get(ctx, goalID)
	if err != nil {
		return goalnotification.Preference{}, err
	}
	if pref == nil {
		return goalnotification.Default(goalID, userID), nil
	}
	return *pref, nil
}

// UpdateGoalNotificationPreferenceInput carries the fields a PATCH request may change.
type UpdateGoalNotificationPreferenceInput struct {
	Muted           *bool
	DigestFrequency *string
}

// Update applies a partial update to the goal's notification preference,
// creating the row on first use.
func (s *GoalNotificationPreferenceService) Update(ctx context.Context, userID, goalID uuid.UUID, in UpdateGoalNotificationPreferenceInput) (goalnotification.Preference, error) {
	if err := s.mustOwnGoal(ctx, userID, goalID); err != nil {
		return goalnotification.Preference{}, err
	}
	current, err := s.repo.Get(ctx, goalID)
	if err != nil {
		return goalnotification.Preference{}, err
	}
	pref := goalnotification.Default(goalID, userID)
	if current != nil {
		pref = *current
	}
	if in.Muted != nil {
		pref.Muted = *in.Muted
	}
	if in.DigestFrequency != nil {
		freq, err := goalnotification.ParseFrequency(*in.DigestFrequency)
		if err != nil {
			return goalnotification.Preference{}, err
		}
		pref.DigestFrequency = freq
	}
	pref.GoalID = goalID
	pref.UserID = userID
	return s.repo.Upsert(ctx, pref)
}

func (s *GoalNotificationPreferenceService) mustOwnGoal(ctx context.Context, userID, goalID uuid.UUID) error {
	goal, err := s.goalRepo.GetByID(ctx, goalID)
	if err != nil {
		return err
	}
	if goal.UserID != userID {
		return savingsgoal.ErrGoalNotFound
	}
	return nil
}
