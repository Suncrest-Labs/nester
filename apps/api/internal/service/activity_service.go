package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/activity"
)

// ActivityService is a thin pass-through over activity.Repository, matching
// the service-per-domain convention used elsewhere (e.g. SettlementService).
type ActivityService struct {
	repo activity.Repository
}

func NewActivityService(repo activity.Repository) *ActivityService {
	return &ActivityService{repo: repo}
}

func (s *ActivityService) List(ctx context.Context, userID uuid.UUID, filter activity.ListFilter) ([]activity.Item, string, string, error) {
	return s.repo.List(ctx, userID, filter)
}
