// Package timeseries stores high-volume metric history and downsampled rollups
// for APY, TVL, and portfolio charting.
package timeseries

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type Metric string

const (
	MetricAPY       Metric = "apy"
	MetricTVL       Metric = "tvl"
	MetricPortfolio Metric = "portfolio"
)

type Resolution string

const (
	ResolutionRaw    Resolution = "raw"
	ResolutionMinute Resolution = "minute"
	ResolutionHour   Resolution = "hour"
	ResolutionDay    Resolution = "day"
)

var ErrInvalidSeries = errors.New("timeseries: invalid series")

type SeriesRef struct {
	Metric     Metric
	EntityType string
	EntityID   string
	Dimensions map[string]string
}

func (s SeriesRef) Key() (string, error) {
	if s.Metric == "" || s.EntityType == "" || s.EntityID == "" {
		return "", ErrInvalidSeries
	}

	parts := []string{string(s.Metric), s.EntityType, s.EntityID}
	if len(s.Dimensions) > 0 {
		keys := make([]string, 0, len(s.Dimensions))
		for key := range s.Dimensions {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parts = append(parts, key+"="+s.Dimensions[key])
		}
	}
	return strings.Join(parts, ":"), nil
}

type Point struct {
	Series     SeriesRef
	ObservedAt time.Time
	Value      decimal.Decimal
}

type RollupPoint struct {
	SeriesKey   string
	Metric      Metric
	EntityType  string
	EntityID    string
	Resolution  Resolution
	BucketStart time.Time
	Open        decimal.Decimal
	High        decimal.Decimal
	Low         decimal.Decimal
	Close       decimal.Decimal
	Average     decimal.Decimal
	Last        decimal.Decimal
	Count       int64
}

type QueryRequest struct {
	Series              SeriesRef
	From                time.Time
	To                  time.Time
	MaxPoints           int
	PreferredResolution Resolution
}

type QueryResult struct {
	Resolution Resolution
	Points     []RollupPoint
}

type RetentionPolicy struct {
	RawMaxAge    time.Duration
	MinuteMaxAge time.Duration
	HourMaxAge   time.Duration
}

func ValidResolution(resolution Resolution) bool {
	switch resolution {
	case ResolutionRaw, ResolutionMinute, ResolutionHour, ResolutionDay:
		return true
	default:
		return false
	}
}

func BucketStart(t time.Time, resolution Resolution) (time.Time, error) {
	t = t.UTC()
	switch resolution {
	case ResolutionRaw:
		return t, nil
	case ResolutionMinute:
		return t.Truncate(time.Minute), nil
	case ResolutionHour:
		return t.Truncate(time.Hour), nil
	case ResolutionDay:
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC), nil
	default:
		return time.Time{}, fmt.Errorf("timeseries: invalid resolution %q", resolution)
	}
}

func SelectTier(req QueryRequest, now time.Time) Resolution {
	if req.PreferredResolution != "" && ValidResolution(req.PreferredResolution) {
		return req.PreferredResolution
	}

	maxPoints := req.MaxPoints
	if maxPoints <= 0 {
		maxPoints = 500
	}

	duration := req.To.Sub(req.From)
	if duration <= 0 {
		return ResolutionRaw
	}
	if !req.From.Before(now.UTC().Add(-24*time.Hour)) && duration <= time.Duration(maxPoints)*time.Minute {
		return ResolutionRaw
	}
	if duration <= time.Duration(maxPoints)*time.Minute {
		return ResolutionMinute
	}
	if duration <= time.Duration(maxPoints)*time.Hour {
		return ResolutionHour
	}
	return ResolutionDay
}
