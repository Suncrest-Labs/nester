package usersignal

import (
	"context"

	"github.com/google/uuid"
)

type SegmentProvider interface {
	DeriveSegment(ctx context.Context, userID uuid.UUID) (Segment, error)
}

type EngagementProvider interface {
	ComputeEngagement(ctx context.Context, userID uuid.UUID) (EngagementScore, EngagementTier, error)
}

type TimingProvider interface {
	InferResponsiveWindow(ctx context.Context, userID uuid.UUID) (ResponsiveWindow, error)
}
