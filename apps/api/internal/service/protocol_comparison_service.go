package service

import (
	"context"
	"errors"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/analytics"
)

// ErrInvalidPeriod is returned when Compare is called with an unsupported period.
var ErrInvalidPeriod = errors.New("period must be '30d' or '90d'")

// ProtocolComparisonService answers "which protocol has been most consistent
// over 30/90 days" (#1324) by aggregating realized APY by protocol over time.
type ProtocolComparisonService struct {
	repo analytics.ComparisonRepository
}

// NewProtocolComparisonService constructs a ProtocolComparisonService.
func NewProtocolComparisonService(repo analytics.ComparisonRepository) *ProtocolComparisonService {
	return &ProtocolComparisonService{repo: repo}
}

// Compare returns the protocol yield time-series comparison for period
// ("30d" or "90d").
func (s *ProtocolComparisonService) Compare(ctx context.Context, period string) (analytics.ComparisonResponse, error) {
	var days int
	switch period {
	case "30d":
		days = 30
	case "90d":
		days = 90
	default:
		return analytics.ComparisonResponse{}, ErrInvalidPeriod
	}

	since := time.Now().UTC().AddDate(0, 0, -days)
	series, err := s.repo.ProtocolYieldHistory(ctx, since)
	if err != nil {
		return analytics.ComparisonResponse{}, err
	}

	return analytics.ComparisonResponse{Period: period, Protocols: series}, nil
}
