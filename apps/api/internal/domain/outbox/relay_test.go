package outbox

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
)

const testJobType = "test_delivery"

// deriveIDGolden pins DeriveID's output for a known input. See
// TestDeriveIDIsStableAcrossProcesses.
const deriveIDGolden = "c94b54bc-7825-538d-b4d6-41872f3fe8a0"

func testRoutes() Routes { return Routes{"test.event": testJobType} }

func newTestRelay(t *testing.T, repo Repository, q *memQueue) (*Relay, *StdMetrics) {
	t.Helper()
	metrics := NewStdMetrics()
	relay := NewRelay(repo, q, q, testRoutes(), RelayConfig{
		Enabled:   true,
		BatchSize: 50,
		Lease:     time.Minute,
		Backoff:   time.Second,
	}, nil, metrics)
	return relay, metrics
}

// publish inserts a pending event. tx is nil because memRepo ignores it;
// against Postgres this argument is the caller's transaction and is the
// entire point of the pattern.
func publish(t *testing.T, repo Repository, aggregateID, dedupeKey string, payload any) Event {
	t.Helper()
	e, err := NewEvent("savings_goal", aggregateID, "test.event", dedupeKey, payload)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	if err := repo.Insert(context.Background(), nil, e); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	return e
}

// TestOutboxSurvivesCrashBetweenWriteAndDispatch is the acceptance criterion
// "killing the process between domain write and dispatch results in the side
// effect being delivered on restart". The crash is modelled the only way it
// can be: the event is committed, no relay ever ran, and then a brand-new
// relay starts against the same store.
func TestOutboxSurvivesCrashBetweenWriteAndDispatch(t *testing.T) {
	repo := newMemRepo()
	e := publish(t, repo, "goal-1", "dedupe-crash-1", map[string]any{"milestone": 50})

	// --- process dies here: the row is committed, nothing was dispatched ---
	if got := repo.byID(e.ID).Status; got != StatusPending {
		t.Fatalf("status before restart = %q, want %q", got, StatusPending)
	}

	q := newMemQueue()
	relay, metrics := newTestRelay(t, repo, q)
	ctx := context.Background()

	if n := relay.Tick(ctx); n != 1 {
		t.Fatalf("first tick moved %d events, want 1", n)
	}
	if q.count() != 1 {
		t.Fatalf("enqueued %d jobs, want 1", q.count())
	}
	if got := repo.byID(e.ID).Status; got != StatusDispatching {
		t.Fatalf("status after hand-off = %q, want %q", got, StatusDispatching)
	}

	q.finish(jobqueue.StatusSucceeded)
	relay.Tick(ctx)

	if got := repo.byID(e.ID).Status; got != StatusDispatched {
		t.Fatalf("status after delivery = %q, want %q", got, StatusDispatched)
	}
	if got := metrics.Snapshot().Dispatched["test.event"]; got != 1 {
		t.Fatalf("dispatched metric = %d, want 1", got)
	}
}

// TestRelayReclaimsEventAbandonedMidHandoff covers the other crash window:
// the relay claimed the row and died before it recorded a job. No job owns
// the delivery, so the lease has to make the row claimable again — otherwise
// the side effect is lost exactly as surely as if it had never been written.
func TestRelayReclaimsEventAbandonedMidHandoff(t *testing.T) {
	repo := newMemRepo()
	e := publish(t, repo, "goal-1", "dedupe-abandoned", nil)

	now := time.Now()
	// Claim without ever marking dispatching — the abandoned state.
	if _, err := repo.ClaimDue(context.Background(), ClaimParams{Limit: 10, Lease: time.Minute, Now: now}); err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if got := repo.byID(e.ID); got.Status != StatusDispatching || got.JobID != nil {
		t.Fatalf("setup: status=%q job=%v, want dispatching with no job", got.Status, got.JobID)
	}

	q := newMemQueue()
	relay, _ := newTestRelay(t, repo, q)
	ctx := context.Background()

	// Before the lease lapses the row must stay put: a relay that is merely
	// slow must not have its work stolen and double-dispatched.
	relay.SetClock(func() time.Time { return now.Add(30 * time.Second) })
	relay.Tick(ctx)
	if q.count() != 0 {
		t.Fatalf("enqueued %d jobs before lease expiry, want 0", q.count())
	}

	relay.SetClock(func() time.Time { return now.Add(2 * time.Minute) })
	relay.Tick(ctx)
	if q.count() != 1 {
		t.Fatalf("enqueued %d jobs after lease expiry, want 1", q.count())
	}
}

// TestRollbackLeavesNoOutboxRow is the acceptance criterion "rolling back the
// domain transaction leaves no outbox row". It needs a real transaction to
// mean anything, so it runs against Postgres when TEST_DATABASE_DSN is set;
// the in-memory repo cannot roll anything back and would prove nothing.
// See outbox_repository_integration_test.go for the executable version.

// TestPoisonEventDoesNotBlockOtherAggregates is the acceptance criterion "a
// consumer that fails permanently does not block delivery for other
// aggregates" — and the reason the ordering guarantee is per-aggregate
// rather than global.
func TestPoisonEventDoesNotBlockOtherAggregates(t *testing.T) {
	repo := newMemRepo()
	poison := publish(t, repo, "goal-poison", "dedupe-poison", nil)
	healthy := publish(t, repo, "goal-healthy", "dedupe-healthy", nil)

	q := newMemQueue()
	relay, metrics := newTestRelay(t, repo, q)
	ctx := context.Background()

	relay.Tick(ctx)
	if q.count() != 2 {
		t.Fatalf("enqueued %d jobs, want 2 (one per aggregate)", q.count())
	}

	// The poison consumer fails permanently; the healthy one succeeds. The
	// queue reports each independently.
	q.mu.Lock()
	for _, id := range q.order {
		job := q.jobs[id]
		if string(job.IdempotencyKey) == "dedupe-poison" {
			job.Status = jobqueue.StatusDead
		} else {
			job.Status = jobqueue.StatusSucceeded
		}
	}
	q.mu.Unlock()

	relay.Tick(ctx)

	if got := repo.byID(poison.ID).Status; got != StatusDead {
		t.Fatalf("poison event status = %q, want %q", got, StatusDead)
	}
	if got := repo.byID(healthy.ID).Status; got != StatusDispatched {
		t.Fatalf("healthy event status = %q, want %q", got, StatusDispatched)
	}
	if got := metrics.Snapshot().DeadLettered["test.event"]; got != 1 {
		t.Fatalf("dead-letter metric = %d, want 1", got)
	}
}

// TestPerAggregateOrderingIsPreserved is the acceptance criterion "ordering
// guarantee is documented and asserted". Within one aggregate, event N+1 is
// not handed over until event N is terminal.
func TestPerAggregateOrderingIsPreserved(t *testing.T) {
	repo := newMemRepo()
	first := publish(t, repo, "goal-1", "dedupe-1", map[string]any{"n": 1})
	second := publish(t, repo, "goal-1", "dedupe-2", map[string]any{"n": 2})
	third := publish(t, repo, "goal-1", "dedupe-3", map[string]any{"n": 3})

	q := newMemQueue()
	relay, _ := newTestRelay(t, repo, q)
	ctx := context.Background()

	relay.Tick(ctx)
	if q.count() != 1 {
		t.Fatalf("enqueued %d jobs on first tick, want 1 — later events must wait", q.count())
	}
	// Ticking again changes nothing: the head is still in flight.
	relay.Tick(ctx)
	if q.count() != 1 {
		t.Fatalf("enqueued %d jobs while head in flight, want 1", q.count())
	}
	if got := repo.byID(second.ID).Status; got != StatusPending {
		t.Fatalf("second event status = %q, want %q", got, StatusPending)
	}

	q.finish(jobqueue.StatusSucceeded)
	relay.Tick(ctx)
	if q.count() != 2 {
		t.Fatalf("enqueued %d jobs after head delivered, want 2", q.count())
	}

	q.finish(jobqueue.StatusSucceeded)
	relay.Tick(ctx)
	if q.count() != 3 {
		t.Fatalf("enqueued %d jobs after second delivered, want 3", q.count())
	}

	q.finish(jobqueue.StatusSucceeded)
	relay.Tick(ctx)

	for _, e := range []Event{first, second, third} {
		if got := repo.byID(e.ID).Status; got != StatusDispatched {
			t.Fatalf("event %s status = %q, want %q", e.DedupeKey, got, StatusDispatched)
		}
	}

	want := []string{`{"n":1}`, `{"n":2}`, `{"n":3}`}
	got := q.payloads()
	if len(got) != len(want) {
		t.Fatalf("payload count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("payload[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

// TestPoisonEventUnblocksItsOwnAggregate pins the interaction the issue calls
// out explicitly: per-aggregate ordering plus dead-lettering. A permanently
// failing event stalls its aggregate only until it dead-letters, at which
// point the events behind it proceed.
func TestPoisonEventUnblocksItsOwnAggregate(t *testing.T) {
	repo := newMemRepo()
	poison := publish(t, repo, "goal-1", "dedupe-head-poison", nil)
	behind := publish(t, repo, "goal-1", "dedupe-behind", nil)

	q := newMemQueue()
	relay, _ := newTestRelay(t, repo, q)
	ctx := context.Background()

	relay.Tick(ctx)
	q.finish(jobqueue.StatusDead)
	relay.Tick(ctx)

	if got := repo.byID(poison.ID).Status; got != StatusDead {
		t.Fatalf("poison status = %q, want %q", got, StatusDead)
	}
	if got := repo.byID(behind.ID).Status; got == StatusPending {
		t.Fatalf("event behind poison is still pending; the aggregate never unblocked")
	}
	if q.count() != 2 {
		t.Fatalf("enqueued %d jobs, want 2 — the event behind the poison must proceed", q.count())
	}
}

// TestDedupeKeyIsTheQueueIdempotencyKey is the acceptance criterion
// "duplicate delivery carries a stable dedupe key". Re-handing the same event
// over must join the existing live job rather than create a second one.
func TestDedupeKeyIsTheQueueIdempotencyKey(t *testing.T) {
	repo := newMemRepo()
	e := publish(t, repo, "goal-1", "stable-dedupe-key", nil)

	q := newMemQueue()
	relay, _ := newTestRelay(t, repo, q)
	ctx := context.Background()
	now := time.Now()
	relay.SetClock(func() time.Time { return now })

	relay.Tick(ctx)

	// Simulate the record-the-job-id write failing: the job exists, the
	// outbox row does not know about it, and the lease will expire.
	stored := repo.byID(e.ID)
	if stored.JobID == nil {
		t.Fatal("setup: expected a job id to have been recorded")
	}
	repo.mu.Lock()
	repo.events[e.ID].JobID = nil
	repo.mu.Unlock()

	relay.SetClock(func() time.Time { return now.Add(2 * time.Minute) })
	relay.Tick(ctx)

	if q.count() != 1 {
		t.Fatalf("enqueued %d jobs, want 1 — the dedupe key must join the live job", q.count())
	}
	if keys := q.idempotencyKeys(); len(keys) != 1 || keys[0] != "stable-dedupe-key" {
		t.Fatalf("idempotency keys = %v, want [stable-dedupe-key]", keys)
	}
}

// TestUnroutableEventDeadLetters: an event type with no registered handler
// cannot be delivered by retrying, so it must dead-letter immediately rather
// than block its aggregate forever.
func TestUnroutableEventDeadLetters(t *testing.T) {
	repo := newMemRepo()
	e, err := NewEvent("savings_goal", "goal-1", "unknown.event", "dedupe-unroutable", nil)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	if err := repo.Insert(context.Background(), nil, e); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	q := newMemQueue()
	relay, metrics := newTestRelay(t, repo, q)
	relay.Tick(context.Background())

	got := repo.byID(e.ID)
	if got.Status != StatusDead {
		t.Fatalf("status = %q, want %q", got.Status, StatusDead)
	}
	if got.LastError == "" {
		t.Fatal("dead-lettered event recorded no reason")
	}
	if q.count() != 0 {
		t.Fatalf("enqueued %d jobs for an unroutable event, want 0", q.count())
	}
	if n := metrics.Snapshot().DeadLettered["unknown.event"]; n != 1 {
		t.Fatalf("dead-letter metric = %d, want 1", n)
	}
}

// TestEnqueueFailureIsRetriedThenDeadLettered: a hand-off that keeps failing
// is bounded by MaxAttempts. The bound exists so a row that can never be
// enqueued stops holding its aggregate open.
func TestEnqueueFailureIsRetriedThenDeadLettered(t *testing.T) {
	repo := newMemRepo()
	e, err := NewEvent("savings_goal", "goal-1", "test.event", "dedupe-enqueue-fail", nil)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	e.MaxAttempts = 3
	if err := repo.Insert(context.Background(), nil, e); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	q := newMemQueue()
	q.failWith = errQueueDown
	relay, _ := newTestRelay(t, repo, q)
	ctx := context.Background()

	now := time.Now()
	for i := 0; i < 3; i++ {
		relay.SetClock(func() time.Time { return now })
		relay.Tick(ctx)
		now = now.Add(time.Minute)
	}

	got := repo.byID(e.ID)
	if got.Status != StatusDead {
		t.Fatalf("status = %q after exhausting hand-off attempts, want %q", got.Status, StatusDead)
	}
	if got.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", got.Attempts)
	}
	if got.LastError == "" {
		t.Fatal("dead-lettered event recorded no reason")
	}
}

// TestHandoffAbandonedOnItsFinalAttemptIsDeadLettered closes the stuck-row
// case: the relay dies mid-hand-off on the last attempt the budget allows.
// The row is left claimed with no job carrying it, and if the claim declined
// to pick it back up it would block its aggregate forever. It must be
// reclaimed and dead-lettered instead.
func TestHandoffAbandonedOnItsFinalAttemptIsDeadLettered(t *testing.T) {
	repo := newMemRepo()
	e, err := NewEvent("savings_goal", "goal-1", "test.event", "dedupe-final-attempt", nil)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	e.MaxAttempts = 1
	if err := repo.Insert(context.Background(), nil, e); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	behind := publish(t, repo, "goal-1", "dedupe-behind-final", nil)

	now := time.Now()
	// Claim without recording a job: the relay died mid-hand-off. attempts
	// is now 1, which is max_attempts.
	if _, err := repo.ClaimDue(context.Background(), ClaimParams{Limit: 10, Lease: time.Minute, Now: now}); err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}

	q := newMemQueue()
	relay, _ := newTestRelay(t, repo, q)
	relay.SetClock(func() time.Time { return now.Add(2 * time.Minute) })
	relay.Tick(context.Background())

	if got := repo.byID(e.ID).Status; got != StatusDead {
		t.Fatalf("status = %q, want %q — an exhausted hand-off must not stay claimed", got, StatusDead)
	}

	// And the aggregate is unblocked: the event behind it proceeds.
	relay.Tick(context.Background())
	if got := repo.byID(behind.ID).Status; got == StatusPending {
		t.Fatal("the event behind an exhausted hand-off is still pending; the aggregate never unblocked")
	}
}

// TestVanishedJobIsHandedOverAgain: if the job row carrying a delivery is
// gone, nothing is delivering it. Leaving the event in flight would block its
// aggregate forever, so it goes back to pending.
func TestVanishedJobIsHandedOverAgain(t *testing.T) {
	repo := newMemRepo()
	e := publish(t, repo, "goal-1", "dedupe-vanished", nil)

	q := newMemQueue()
	relay, _ := newTestRelay(t, repo, q)
	ctx := context.Background()
	now := time.Now()
	relay.SetClock(func() time.Time { return now })

	relay.Tick(ctx)
	jobID := repo.byID(e.ID).JobID
	if jobID == nil {
		t.Fatal("setup: no job recorded")
	}

	q.remove(*jobID)

	relay.Tick(ctx)
	if got := repo.byID(e.ID).Status; got != StatusPending {
		t.Fatalf("status = %q after job vanished, want %q", got, StatusPending)
	}

	relay.SetClock(func() time.Time { return now.Add(time.Minute) })
	relay.Tick(ctx)
	if q.count() != 2 {
		t.Fatalf("enqueued %d jobs, want 2 — the event must be handed over again", q.count())
	}
}

// TestClaimFailureIsNotFatal: a store error during claim must not wedge the
// relay or lose the event; the next tick picks it up.
func TestClaimFailureIsNotFatal(t *testing.T) {
	repo := newMemRepo()
	e := publish(t, repo, "goal-1", "dedupe-claim-fail", nil)
	repo.failClaim = errors.New("connection reset")

	q := newMemQueue()
	relay, _ := newTestRelay(t, repo, q)
	ctx := context.Background()

	if n := relay.Tick(ctx); n != 0 {
		t.Fatalf("tick moved %d events during a store failure, want 0", n)
	}
	repo.failClaim = nil
	if n := relay.Tick(ctx); n != 1 {
		t.Fatalf("tick moved %d events after recovery, want 1", n)
	}
	if got := repo.byID(e.ID).Status; got != StatusDispatching {
		t.Fatalf("status = %q, want %q", got, StatusDispatching)
	}
}

// TestRunStopsOnContextCancel guards the loop's shutdown path.
func TestRunStopsOnContextCancel(t *testing.T) {
	repo := newMemRepo()
	q := newMemQueue()
	relay, _ := newTestRelay(t, repo, q)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- relay.Run(ctx) }()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

// TestDisabledRelayDoesNothing pins the kill switch.
func TestDisabledRelayDoesNothing(t *testing.T) {
	repo := newMemRepo()
	publish(t, repo, "goal-1", "dedupe-disabled", nil)
	q := newMemQueue()
	relay := NewRelay(repo, q, q, testRoutes(), RelayConfig{Enabled: false}, nil, nil)

	if err := relay.Run(context.Background()); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if q.count() != 0 {
		t.Fatalf("disabled relay enqueued %d jobs, want 0", q.count())
	}
}

// TestStatsSurfaceOldestPendingAge: depth alone hides a wedged relay, because
// a relay that has stopped relaying holds a constant depth. Age does not.
func TestStatsSurfaceOldestPendingAge(t *testing.T) {
	repo := newMemRepo()
	e := publish(t, repo, "goal-1", "dedupe-stats", nil)
	repo.mu.Lock()
	repo.events[e.ID].CreatedAt = time.Now().Add(-90 * time.Minute)
	repo.mu.Unlock()

	q := newMemQueue()
	relay, metrics := newTestRelay(t, repo, q)
	relay.refreshStats(context.Background())

	snap := metrics.Snapshot()
	if snap.PendingDepth != 1 {
		t.Fatalf("pending depth = %d, want 1", snap.PendingDepth)
	}
	if snap.OldestPendingAge < 89*time.Minute {
		t.Fatalf("oldest pending age = %s, want >= 89m", snap.OldestPendingAge)
	}
}

func TestEventValidate(t *testing.T) {
	tests := []struct {
		name    string
		event   Event
		wantErr bool
	}{
		{"valid", Event{AggregateType: "a", AggregateID: "b", EventType: "c", DedupeKey: "d"}, false},
		{"no aggregate type", Event{AggregateID: "b", EventType: "c", DedupeKey: "d"}, true},
		{"no aggregate id", Event{AggregateType: "a", EventType: "c", DedupeKey: "d"}, true},
		{"no event type", Event{AggregateType: "a", AggregateID: "b", DedupeKey: "d"}, true},
		{"no dedupe key", Event{AggregateType: "a", AggregateID: "b", EventType: "c"}, true},
		{
			"invalid payload",
			Event{AggregateType: "a", AggregateID: "b", EventType: "c", DedupeKey: "d", Payload: []byte(`{oops`)},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.event.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestDeriveIDIsStableAcrossProcesses pins the determinism the whole dedupe
// story rests on: the same key must yield the same id in a future process,
// or a consumer cannot recognise a redelivery as one.
func TestDeriveIDIsStableAcrossProcesses(t *testing.T) {
	const key = "goal-1:milestone:50"
	a := DeriveID(key, "webhook-abc")
	b := DeriveID(key, "webhook-abc")
	if a != b {
		t.Fatalf("DeriveID is not deterministic: %s != %s", a, b)
	}
	if a == uuid.Nil {
		t.Fatal("DeriveID returned the nil UUID")
	}
	if c := DeriveID(key, "webhook-xyz"); a == c {
		t.Fatal("different scopes must derive different ids")
	}
	if d := DeriveID("different-key", "webhook-abc"); a == d {
		t.Fatal("different dedupe keys must derive different ids")
	}
	// Pinned literal, because determinism within one build proves nothing:
	// changing the namespace or the scope separator would still pass the
	// checks above while silently invalidating every consumer's dedupe
	// store, making previously-seen deliveries look new.
	if got := DeriveID("goal-1:milestone:50", "webhook-abc").String(); got != deriveIDGolden {
		t.Fatalf("DeriveID drifted: got %s, want %s — this invalidates consumer dedupe stores", got, deriveIDGolden)
	}
}

// TestWriterRejectsInvalidEvents: producers get the error inside their own
// transaction, where they can still roll the domain write back, rather than
// as a dead-letter discovered hours later.
func TestWriterRejectsInvalidEvents(t *testing.T) {
	w := NewWriter(newMemRepo())
	err := w.Publish(context.Background(), nilExecer{}, "savings_goal", "goal-1", "test.event", "", nil)
	if err == nil {
		t.Fatal("Publish with an empty dedupe key succeeded, want an error")
	}
}

func TestWriterWithoutRepositoryFails(t *testing.T) {
	w := NewWriter(nil)
	if err := w.Publish(context.Background(), nilExecer{}, "a", "b", "c", "d", nil); err == nil {
		t.Fatal("Publish without a repository succeeded, want an error")
	}
}

// nilExecer satisfies Execer without a database; memRepo ignores the handle.
type nilExecer struct{}

func (nilExecer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}
