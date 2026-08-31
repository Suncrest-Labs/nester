package stellar

import (
	"context"
	"errors"
	"testing"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/systemstate"
)

// recordingRecorder captures what the indexer publishes, so the sampling
// contract can be asserted without a database or a Prometheus registry.
type recordingRecorder struct {
	samples  [][2]uint64
	failures int
}

func (r *recordingRecorder) Observe(indexedLedger, networkLedger uint64) {
	r.samples = append(r.samples, [2]uint64{indexedLedger, networkLedger})
}

func (r *recordingRecorder) ObserveFailure() {
	r.failures++
}

func TestSampleFreshnessPublishesCursorAndTip(t *testing.T) {
	sysRepo := newStubSysRepo()
	sysRepo.values[systemstate.KeyLastLedger] = "1000"
	recorder := &recordingRecorder{}

	sampleFreshness(context.Background(), sysRepo, &scriptedFetcher{tip: 1_007}, recorder)

	if len(recorder.samples) != 1 {
		t.Fatalf("samples = %d, want 1", len(recorder.samples))
	}
	if got := recorder.samples[0]; got != [2]uint64{1_000, 1_007} {
		t.Fatalf("sample = %v, want [1000 1007]", got)
	}
	if recorder.failures != 0 {
		t.Fatalf("failures = %d, want 0", recorder.failures)
	}
}

// A cursor that has never been set is not "zero ledgers behind": it is no
// information at all. Publishing tip-minus-zero would report the entire ledger
// history as lag and page on every fresh deploy, so it is recorded as a
// failed sample and the freshness signal simply keeps ageing.
func TestSampleFreshnessTreatsAnUninitialisedCursorAsUnknown(t *testing.T) {
	cases := map[string]*stubSysRepo{
		"absent cursor": newStubSysRepo(),
		"cursor seeded at zero by migration 025": func() *stubSysRepo {
			repo := newStubSysRepo()
			repo.values[systemstate.KeyLastLedger] = "0"
			return repo
		}(),
	}

	for name, sysRepo := range cases {
		t.Run(name, func(t *testing.T) {
			recorder := &recordingRecorder{}

			sampleFreshness(context.Background(), sysRepo, &scriptedFetcher{tip: 493_812}, recorder)

			if len(recorder.samples) != 0 {
				t.Fatalf("published a position with no cursor: %v", recorder.samples)
			}
			if recorder.failures != 1 {
				t.Fatalf("failures = %d, want 1", recorder.failures)
			}
		})
	}
}

// An unreachable RPC means the tip is unknown, which is not the same as the
// indexer being caught up. Recording a failure leaves the last good reading in
// place to keep ageing, rather than writing a sentinel that would be
// indistinguishable from a real stall.
func TestSampleFreshnessRecordsFailureWhenTheTipIsUnavailable(t *testing.T) {
	sysRepo := newStubSysRepo()
	sysRepo.values[systemstate.KeyLastLedger] = "1000"
	recorder := &recordingRecorder{}

	fetcher := &scriptedFetcher{tipErr: errors.New("rpc unreachable")}
	sampleFreshness(context.Background(), sysRepo, fetcher, recorder)

	if len(recorder.samples) != 0 {
		t.Fatalf("published a position without a tip: %v", recorder.samples)
	}
	if recorder.failures != 1 {
		t.Fatalf("failures = %d, want 1", recorder.failures)
	}
}

// An RPC reporting ledger 0 is broken, not a real position. Publishing it
// would compute zero lag against any cursor and reset the sample age, so the
// indexer would report as perfectly fresh while its position relative to the
// network is unknown.
func TestSampleFreshnessRejectsAZeroTip(t *testing.T) {
	sysRepo := newStubSysRepo()
	sysRepo.values[systemstate.KeyLastLedger] = "1000"
	recorder := &recordingRecorder{}

	sampleFreshness(context.Background(), sysRepo, &scriptedFetcher{tip: 0}, recorder)

	if len(recorder.samples) != 0 {
		t.Fatalf("published a position against a zero tip: %v", recorder.samples)
	}
	if recorder.failures != 1 {
		t.Fatalf("failures = %d, want 1", recorder.failures)
	}
}

// A database that cannot serve the cursor is the same kind of unknown.
func TestSampleFreshnessRecordsFailureWhenTheCursorCannotBeRead(t *testing.T) {
	sysRepo := newStubSysRepo()
	sysRepo.getErr = errors.New("connection refused")
	recorder := &recordingRecorder{}

	sampleFreshness(context.Background(), sysRepo, &scriptedFetcher{tip: 1_007}, recorder)

	if recorder.failures != 1 {
		t.Fatalf("failures = %d, want 1", recorder.failures)
	}
}

// Telemetry must never be able to stop the indexer from indexing, so a
// deployment wired without a recorder samples nothing and does not panic.
func TestSampleFreshnessWithNilRecorderIsANoOp(t *testing.T) {
	sysRepo := newStubSysRepo()
	sysRepo.values[systemstate.KeyLastLedger] = "1000"

	sampleFreshness(context.Background(), sysRepo, &scriptedFetcher{tip: 1_007}, nil)
}

// An indexer momentarily ahead of the reported tip is normal — the cursor
// advances between the tip read and the next sample. The position is published
// as-is; clamping to zero lag is the freshness model's job, not the sampler's,
// so both consumers see the same raw numbers.
func TestSampleFreshnessPublishesAPositionAheadOfTheTip(t *testing.T) {
	sysRepo := newStubSysRepo()
	sysRepo.values[systemstate.KeyLastLedger] = "1010"
	recorder := &recordingRecorder{}

	sampleFreshness(context.Background(), sysRepo, &scriptedFetcher{tip: 1_007}, recorder)

	if len(recorder.samples) != 1 {
		t.Fatalf("samples = %d, want 1", len(recorder.samples))
	}
	if got := recorder.samples[0]; got != [2]uint64{1_010, 1_007} {
		t.Fatalf("sample = %v, want [1010 1007]", got)
	}
}
