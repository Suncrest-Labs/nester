package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/outbox"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
)

// outboxHarness is the production relay-plus-worker pair, driven a tick at a
// time instead of on timers so integration tests are deterministic rather
// than sleep-and-hope.
type outboxHarness struct {
	relay    *outbox.Relay
	jobs     *postgres.JobRepository
	outbox   *postgres.OutboxRepository
	handlers map[string]jobqueue.Handler
}

// newOutboxHarness wires the same relay, routes, and job repository main.go
// does. Constructing it AFTER the domain write is deliberate in these tests:
// it is how a restart is modelled — the process that wrote the row is gone,
// and a fresh one picks the work up.
func newOutboxHarness(t *testing.T, db *sql.DB, handlers map[string]jobqueue.Handler) *outboxHarness {
	t.Helper()
	jobRepo := postgres.NewJobRepository(db)
	outboxRepo := postgres.NewOutboxRepository(db)

	routes := outbox.Routes{}
	for jobType := range handlers {
		switch jobType {
		case GoalMilestoneJobType:
			routes[OutboxEventGoalMilestone] = jobType
		case WebhookFanoutJobType:
			routes[OutboxEventWebhookFanout] = jobType
		}
	}

	relay := outbox.NewRelay(outboxRepo, jobqueue.NewClient(jobRepo, nil), jobRepo, routes,
		outbox.RelayConfig{Enabled: true, BatchSize: 50, Lease: time.Minute, Backoff: time.Second},
		nil, nil)

	return &outboxHarness{relay: relay, jobs: jobRepo, outbox: outboxRepo, handlers: handlers}
}

// drain runs relay ticks and job executions until the outbox is quiet, up to
// maxRounds. Each round is one full cycle of the production loop: reconcile
// finished jobs, hand over each aggregate's next event, run what was handed
// over. Because ordering is per-aggregate, a goal with four milestones needs
// four rounds — which is precisely the guarantee being exercised.
func (h *outboxHarness) drain(ctx context.Context, t *testing.T, maxRounds int) {
	t.Helper()
	for i := 0; i < maxRounds; i++ {
		moved := h.relay.Tick(ctx)
		ran := h.runJobs(ctx, t)
		// Reconcile the jobs just run so the next aggregate head unblocks.
		h.relay.Tick(ctx)
		if moved == 0 && ran == 0 {
			return
		}
	}
}

// runJobs executes every runnable job of every registered type, exactly as
// the worker pool would: lease, handle, then record the terminal state.
func (h *outboxHarness) runJobs(ctx context.Context, t *testing.T) int {
	t.Helper()
	ran := 0
	for jobType, handler := range h.handlers {
		jobs, err := h.jobs.Dequeue(ctx, jobqueue.DequeueParams{
			Type: jobType, Limit: 50, Lease: time.Minute, Now: time.Now(),
		})
		if err != nil {
			t.Fatalf("dequeue %s: %v", jobType, err)
		}
		for _, job := range jobs {
			ran++
			if err := handler.Handle(ctx, job); err != nil {
				if dErr := h.jobs.DeadLetter(ctx, job.ID, err.Error()); dErr != nil {
					t.Fatalf("dead-letter %s: %v", job.ID, dErr)
				}
				continue
			}
			if err := h.jobs.Complete(ctx, job.ID, nil); err != nil {
				t.Fatalf("complete %s: %v", job.ID, err)
			}
		}
	}
	return ran
}

// pendingOutboxCount counts events not yet delivered — the backlog a crash
// would have left behind.
func pendingOutboxCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM outbox WHERE status IN ('pending', 'dispatching')`,
	).Scan(&n); err != nil {
		t.Fatalf("count pending outbox rows: %v", err)
	}
	return n
}

// dispatchedOutboxCount counts events whose delivery job succeeded.
func dispatchedOutboxCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM outbox WHERE status = 'dispatched'`).Scan(&n); err != nil {
		t.Fatalf("count dispatched outbox rows: %v", err)
	}
	return n
}
