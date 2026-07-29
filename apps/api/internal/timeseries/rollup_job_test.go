package timeseries

import (
	"context"
	"testing"
	"time"
)

type fakeStore struct {
	checkpoints map[Resolution]time.Time
	rollups     []Resolution
	deletedAt   time.Time
}

func (s *fakeStore) Rollup(ctx context.Context, resolution Resolution, from, to time.Time) error {
	s.rollups = append(s.rollups, resolution)
	return nil
}

func (s *fakeStore) Checkpoint(ctx context.Context, resolution Resolution) (time.Time, bool, error) {
	v, ok := s.checkpoints[resolution]
	return v, ok, nil
}

func (s *fakeStore) SetCheckpoint(ctx context.Context, resolution Resolution, processedUntil time.Time) error {
	s.checkpoints[resolution] = processedUntil
	return nil
}

func (s *fakeStore) SafeDeleteRawBefore(ctx context.Context, before time.Time) (int64, error) {
	s.deletedAt = before
	return 1, nil
}

func TestRollupJobResumesFromCheckpointAndRetainsAfterRollup(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 34, 56, 0, time.UTC)
	store := &fakeStore{
		checkpoints: map[Resolution]time.Time{
			ResolutionMinute: now.Add(-2 * time.Hour),
		},
	}
	job := NewRollupJob(store, RollupJobConfig{
		Lookback:     6 * time.Hour,
		RawRetention: 7 * 24 * time.Hour,
		Now:          func() time.Time { return now },
	})

	if err := job.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if len(store.rollups) != 3 {
		t.Fatalf("rollups = %v, want three resolutions", store.rollups)
	}
	if got := store.checkpoints[ResolutionMinute]; !got.Equal(now.Truncate(time.Minute)) {
		t.Fatalf("minute checkpoint = %s", got)
	}
	if store.deletedAt.IsZero() {
		t.Fatal("SafeDeleteRawBefore was not called")
	}
	if !store.deletedAt.Equal(now.Add(-7 * 24 * time.Hour)) {
		t.Fatalf("deletedAt = %s", store.deletedAt)
	}
}
