package timeseries

import (
	"context"
	"fmt"
	"time"
)

type Store interface {
	Rollup(ctx context.Context, resolution Resolution, from, to time.Time) error
	Checkpoint(ctx context.Context, resolution Resolution) (time.Time, bool, error)
	SetCheckpoint(ctx context.Context, resolution Resolution, processedUntil time.Time) error
	SafeDeleteRawBefore(ctx context.Context, before time.Time) (int64, error)
}

type RollupJobConfig struct {
	Lookback     time.Duration
	RawRetention time.Duration
	Now          func() time.Time
}

type RollupJob struct {
	store Store
	cfg   RollupJobConfig
}

func NewRollupJob(store Store, cfg RollupJobConfig) *RollupJob {
	if cfg.Lookback <= 0 {
		cfg.Lookback = 24 * time.Hour
	}
	if cfg.RawRetention <= 0 {
		cfg.RawRetention = 30 * 24 * time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &RollupJob{store: store, cfg: cfg}
}

func (j *RollupJob) RunOnce(ctx context.Context) error {
	now := j.cfg.Now().UTC()
	if err := j.runResolution(ctx, ResolutionMinute, now.Truncate(time.Minute)); err != nil {
		return err
	}
	if err := j.runResolution(ctx, ResolutionHour, now.Truncate(time.Hour)); err != nil {
		return err
	}
	if err := j.runResolution(ctx, ResolutionDay, dayStart(now)); err != nil {
		return err
	}
	if _, err := j.store.SafeDeleteRawBefore(ctx, now.Add(-j.cfg.RawRetention)); err != nil {
		return err
	}
	return nil
}

func (j *RollupJob) runResolution(ctx context.Context, resolution Resolution, to time.Time) error {
	from, ok, err := j.store.Checkpoint(ctx, resolution)
	if err != nil {
		return err
	}
	if !ok {
		from = to.Add(-j.cfg.Lookback)
	}
	if !from.Before(to) {
		return nil
	}
	if err := j.store.Rollup(ctx, resolution, from, to); err != nil {
		return fmt.Errorf("timeseries: run %s rollup: %w", resolution, err)
	}
	return j.store.SetCheckpoint(ctx, resolution, to)
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
