package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/audit"
)

// fakeActivityEventRepo is an in-memory stand-in for the activity_events
// repository method DataRetentionJob needs (nester#1226).
type fakeActivityEventRepo struct {
	rows          []time.Time // occurred_at of each remaining row
	deleteCutoffs []time.Time
	err           error
}

func (f *fakeActivityEventRepo) DeleteOlderThan(_ context.Context, cutoff time.Time) (int64, error) {
	f.deleteCutoffs = append(f.deleteCutoffs, cutoff)
	if f.err != nil {
		return 0, f.err
	}
	var kept []time.Time
	var deleted int64
	for _, t := range f.rows {
		if t.Before(cutoff) {
			deleted++
			continue
		}
		kept = append(kept, t)
	}
	f.rows = kept
	return deleted, nil
}

// fakeNudgeRetentionRepo is an in-memory stand-in for the nudge_dispatch_log
// repository method DataRetentionJob needs.
type fakeNudgeRetentionRepo struct {
	rows          []time.Time // sent_at of each remaining row
	deleteCutoffs []time.Time
	err           error
}

func (f *fakeNudgeRetentionRepo) DeleteDispatchesOlderThan(_ context.Context, cutoff time.Time) (int64, error) {
	f.deleteCutoffs = append(f.deleteCutoffs, cutoff)
	if f.err != nil {
		return 0, f.err
	}
	var kept []time.Time
	var deleted int64
	for _, t := range f.rows {
		if t.Before(cutoff) {
			deleted++
			continue
		}
		kept = append(kept, t)
	}
	f.rows = kept
	return deleted, nil
}

// fakeAuditLogger records every entry it's asked to log, so tests can assert
// deletion is itself audit-logged (nester#1226 acceptance criterion).
type fakeAuditLogger struct {
	entries []audit.Entry
	err     error
}

func (f *fakeAuditLogger) Log(_ context.Context, entry audit.Entry) error {
	if f.err != nil {
		return f.err
	}
	f.entries = append(f.entries, entry)
	return nil
}

func TestDataRetentionJob_DeletesOnlyRowsPastRetentionWindow(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	activity := &fakeActivityEventRepo{
		rows: []time.Time{
			now.Add(-181 * 24 * time.Hour), // past 180d — must be removed
			now.Add(-179 * 24 * time.Hour), // just inside 180d — must survive
			now.Add(-1 * time.Hour),        // recent — must survive
		},
	}
	nudges := &fakeNudgeRetentionRepo{
		rows: []time.Time{
			now.Add(-200 * 24 * time.Hour), // past 180d — must be removed
			now.Add(-10 * 24 * time.Hour),  // recent — must survive
		},
	}
	audit := &fakeAuditLogger{}

	job := NewDataRetentionJob(activity, nudges, audit, DataRetentionConfig{Now: func() time.Time { return now }}, nil)
	job.Tick(context.Background())

	if len(activity.rows) != 2 {
		t.Fatalf("activity_events: expected 2 rows to survive, got %d", len(activity.rows))
	}
	if len(nudges.rows) != 1 {
		t.Fatalf("nudge_dispatch_log: expected 1 row to survive, got %d", len(nudges.rows))
	}
}

func TestDataRetentionJob_UsesConfiguredRetentionWindows(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	activity := &fakeActivityEventRepo{}
	nudges := &fakeNudgeRetentionRepo{}

	cfg := DataRetentionConfig{
		ActivityEventsRetention: 30 * 24 * time.Hour,
		NudgeDispatchRetention:  90 * 24 * time.Hour,
		Now:                     func() time.Time { return now },
	}
	job := NewDataRetentionJob(activity, nudges, nil, cfg, nil)
	job.Tick(context.Background())

	if len(activity.deleteCutoffs) != 1 || !activity.deleteCutoffs[0].Equal(now.Add(-30*24*time.Hour)) {
		t.Fatalf("activity_events cutoff = %v, want %v", activity.deleteCutoffs, now.Add(-30*24*time.Hour))
	}
	if len(nudges.deleteCutoffs) != 1 || !nudges.deleteCutoffs[0].Equal(now.Add(-90*24*time.Hour)) {
		t.Fatalf("nudge_dispatch_log cutoff = %v, want %v", nudges.deleteCutoffs, now.Add(-90*24*time.Hour))
	}
}

func TestDataRetentionJob_DefaultsRetentionTo180Days(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	activity := &fakeActivityEventRepo{}
	nudges := &fakeNudgeRetentionRepo{}

	job := NewDataRetentionJob(activity, nudges, nil, DataRetentionConfig{Now: func() time.Time { return now }}, nil)
	job.Tick(context.Background())

	want := now.Add(-180 * 24 * time.Hour)
	if !activity.deleteCutoffs[0].Equal(want) {
		t.Fatalf("default activity_events cutoff = %v, want %v", activity.deleteCutoffs[0], want)
	}
	if !nudges.deleteCutoffs[0].Equal(want) {
		t.Fatalf("default nudge_dispatch_log cutoff = %v, want %v", nudges.deleteCutoffs[0], want)
	}
}

func TestDataRetentionJob_AuditLogsEachDeletionWithCountAndCutoff(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	activity := &fakeActivityEventRepo{
		rows: []time.Time{now.Add(-200 * 24 * time.Hour), now.Add(-200 * 24 * time.Hour)},
	}
	nudges := &fakeNudgeRetentionRepo{
		rows: []time.Time{now.Add(-200 * 24 * time.Hour)},
	}
	auditLog := &fakeAuditLogger{}

	job := NewDataRetentionJob(activity, nudges, auditLog, DataRetentionConfig{Now: func() time.Time { return now }}, nil)
	job.Tick(context.Background())

	if len(auditLog.entries) != 2 {
		t.Fatalf("expected 2 audit entries (one per table), got %d", len(auditLog.entries))
	}

	var sawActivity, sawNudges bool
	for _, e := range auditLog.entries {
		if e.Action != "data_retention.delete" {
			t.Errorf("action = %q, want data_retention.delete", e.Action)
		}
		if e.UserID != nil {
			t.Errorf("expected UserID nil (system action), got %v", *e.UserID)
		}
		switch e.EntityType {
		case "activity_events":
			sawActivity = true
			nv, ok := e.NewValue.(map[string]any)
			if !ok || nv["deleted_count"] != int64(2) {
				t.Errorf("activity_events audit NewValue = %#v, want deleted_count=2", e.NewValue)
			}
		case "nudge_dispatch_log":
			sawNudges = true
			nv, ok := e.NewValue.(map[string]any)
			if !ok || nv["deleted_count"] != int64(1) {
				t.Errorf("nudge_dispatch_log audit NewValue = %#v, want deleted_count=1", e.NewValue)
			}
		default:
			t.Errorf("unexpected entity_type %q", e.EntityType)
		}
	}
	if !sawActivity || !sawNudges {
		t.Fatalf("expected an audit entry for both tables, sawActivity=%v sawNudges=%v", sawActivity, sawNudges)
	}
}

func TestDataRetentionJob_DoesNotAuditLogWhenNothingWasDeleted(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	activity := &fakeActivityEventRepo{rows: []time.Time{now}} // recent, survives
	nudges := &fakeNudgeRetentionRepo{}
	auditLog := &fakeAuditLogger{}

	job := NewDataRetentionJob(activity, nudges, auditLog, DataRetentionConfig{Now: func() time.Time { return now }}, nil)
	job.Tick(context.Background())

	if len(auditLog.entries) != 0 {
		t.Fatalf("expected no audit entries when nothing was deleted, got %d", len(auditLog.entries))
	}
}

func TestDataRetentionJob_OneTableFailingDoesNotBlockTheOther(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	activity := &fakeActivityEventRepo{err: context.DeadlineExceeded}
	nudges := &fakeNudgeRetentionRepo{rows: []time.Time{now.Add(-200 * 24 * time.Hour)}}
	auditLog := &fakeAuditLogger{}

	job := NewDataRetentionJob(activity, nudges, auditLog, DataRetentionConfig{Now: func() time.Time { return now }}, nil)
	job.Tick(context.Background())

	if len(nudges.rows) != 0 {
		t.Fatalf("expected nudge_dispatch_log sweep to succeed despite activity_events failing, got %d rows remaining", len(nudges.rows))
	}
	if len(auditLog.entries) != 1 || auditLog.entries[0].EntityType != "nudge_dispatch_log" {
		t.Fatalf("expected exactly one audit entry for the successful table, got %#v", auditLog.entries)
	}
}

func TestDataRetentionJob_SkipsTickWhenNotLeader(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	activity := &fakeActivityEventRepo{rows: []time.Time{now.Add(-200 * 24 * time.Hour)}}
	nudges := &fakeNudgeRetentionRepo{rows: []time.Time{now.Add(-200 * 24 * time.Hour)}}

	job := NewDataRetentionJob(activity, nudges, nil, DataRetentionConfig{Now: func() time.Time { return now }}, nil)
	job.SetLeaderChecker(fakePurgeLeaderChecker{leader: false})
	job.Tick(context.Background())

	if len(activity.deleteCutoffs) != 0 || len(nudges.deleteCutoffs) != 0 {
		t.Fatalf("expected no delete calls while not leader")
	}
}

func TestDataRetentionJob_RunsWhenLeaderCheckerIsNil(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	activity := &fakeActivityEventRepo{rows: []time.Time{now.Add(-200 * 24 * time.Hour)}}
	nudges := &fakeNudgeRetentionRepo{}

	job := NewDataRetentionJob(activity, nudges, nil, DataRetentionConfig{Now: func() time.Time { return now }}, nil)
	job.Tick(context.Background())

	if len(activity.rows) != 0 {
		t.Fatalf("expected the sweep to run with no leader checker wired (nil = always leader), got %d rows remaining", len(activity.rows))
	}
}
