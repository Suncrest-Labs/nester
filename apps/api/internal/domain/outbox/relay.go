package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
)

// Enqueuer is the narrow jobqueue.Client surface the relay needs.
// *jobqueue.Client satisfies it.
type Enqueuer interface {
	Enqueue(ctx context.Context, in jobqueue.EnqueueInput) (jobqueue.Job, error)
}

// JobStatusReader reads back the terminal state of a job the relay handed
// over. The relay holds an aggregate's next event until the job carrying the
// current one resolves, so it has to be able to ask.
type JobStatusReader interface {
	// JobStatus returns the job's current status, or jobqueue.ErrNotFound if
	// the job row no longer exists.
	JobStatus(ctx context.Context, id uuid.UUID) (jobqueue.Status, error)
}

// Routes maps an outbox event type to the job queue type that performs it.
// An event whose type has no route is unroutable — a misconfiguration that
// retrying cannot fix — so it is dead-lettered immediately rather than
// retried forever.
type Routes map[string]string

// RelayConfig controls the relay loop.
type RelayConfig struct {
	// Enabled: when false, Run returns immediately.
	Enabled bool
	// PollInterval is how long to wait between ticks that found no work. A
	// tick that dispatched something polls again immediately.
	PollInterval time.Duration
	// BatchSize caps how many aggregate heads one tick claims.
	BatchSize int
	// Lease is how long a claimed row stays invisible to other relays
	// before it is treated as abandoned mid-hand-off and reclaimed.
	Lease time.Duration
	// Backoff parameterizes the delay before re-attempting a failed
	// hand-off. This is NOT delivery retry — that belongs to the job queue.
	Backoff time.Duration
	// StatsInterval is how often outbox gauges are refreshed. Zero disables.
	StatsInterval time.Duration
}

func (c RelayConfig) withDefaults() RelayConfig {
	if c.PollInterval <= 0 {
		c.PollInterval = time.Second
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.Lease <= 0 {
		c.Lease = 30 * time.Second
	}
	if c.Backoff <= 0 {
		c.Backoff = 5 * time.Second
	}
	return c
}

// Relay moves events from the outbox onto the durable job queue and holds
// each aggregate's queue in order while it does.
//
// One tick is two phases, in this order:
//
//  1. Reconcile. Every in-flight event is checked against the job carrying
//     it. A succeeded job dispatches the event; a dead job dead-letters it
//     (the poison path); a job still running leaves the event — and so its
//     whole aggregate — where it is. Reconciling first means an aggregate
//     unblocked by this tick can be claimed by the same tick.
//  2. Claim and hand over. Each claimed event is enqueued under a job type
//     from Routes, keyed on the event's dedupe key so re-handing the same
//     event over cannot produce a second live job.
//
// Multiple relay instances are safe and expected to run concurrently:
// ClaimDue takes row locks with SKIP LOCKED, so instances divide the work
// rather than duplicating it.
type Relay struct {
	repo    Repository
	enqueue Enqueuer
	jobs    JobStatusReader
	routes  Routes
	cfg     RelayConfig
	logger  *slog.Logger
	metrics Metrics
	clock   func() time.Time
}

// NewRelay constructs a Relay. logger and metrics may be nil.
func NewRelay(
	repo Repository,
	enqueue Enqueuer,
	jobs JobStatusReader,
	routes Routes,
	cfg RelayConfig,
	logger *slog.Logger,
	metrics Metrics,
) *Relay {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	if metrics == nil {
		metrics = NoopMetrics{}
	}
	if routes == nil {
		routes = Routes{}
	}
	return &Relay{
		repo:    repo,
		enqueue: enqueue,
		jobs:    jobs,
		routes:  routes,
		cfg:     cfg.withDefaults(),
		logger:  logger,
		metrics: metrics,
		clock:   time.Now,
	}
}

// SetClock overrides the relay's clock (tests only).
func (r *Relay) SetClock(clock func() time.Time) {
	if clock != nil {
		r.clock = clock
	}
}

// Run drives the relay until ctx is cancelled.
func (r *Relay) Run(ctx context.Context) error {
	if !r.cfg.Enabled {
		r.logger.Info("outbox relay disabled; not starting")
		return nil
	}
	r.logger.Info("outbox relay starting",
		"poll_interval", r.cfg.PollInterval,
		"batch_size", r.cfg.BatchSize,
		"routes", len(r.routes),
	)

	poll := time.NewTimer(0)
	defer poll.Stop()

	var statsC <-chan time.Time
	if r.cfg.StatsInterval > 0 {
		stats := time.NewTicker(r.cfg.StatsInterval)
		defer stats.Stop()
		statsC = stats.C
		r.refreshStats(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("outbox relay stopped")
			return nil
		case <-statsC:
			r.refreshStats(ctx)
		case <-poll.C:
			worked := r.Tick(ctx)
			if worked > 0 {
				poll.Reset(0)
			} else {
				poll.Reset(r.cfg.PollInterval)
			}
		}
	}
}

// Tick runs one reconcile-then-claim pass and returns how many events it
// moved. Exported so tests can drive the relay deterministically instead of
// racing its timer.
func (r *Relay) Tick(ctx context.Context) int {
	return r.reconcile(ctx) + r.dispatch(ctx)
}

// reconcile resolves in-flight events against the jobs carrying them, which
// is what releases each aggregate's next event.
func (r *Relay) reconcile(ctx context.Context) int {
	if r.jobs == nil {
		return 0
	}
	inFlight, err := r.repo.InFlight(ctx, r.cfg.BatchSize)
	if err != nil {
		if ctx.Err() == nil {
			r.logger.Error("outbox relay: list in-flight failed", "error", err)
		}
		return 0
	}

	resolved := 0
	for _, e := range inFlight {
		if e.JobID == nil {
			continue
		}
		status, err := r.jobs.JobStatus(ctx, *e.JobID)
		switch {
		case errors.Is(err, jobqueue.ErrNotFound):
			// The job row is gone — pruned, or wiped by an operator — so
			// nothing is carrying this delivery any more. Hand it over
			// again rather than leaving the aggregate blocked forever.
			r.release(ctx, e, fmt.Sprintf("job %s no longer exists", e.JobID))
			resolved++
			continue
		case err != nil:
			if ctx.Err() == nil {
				r.logger.Error("outbox relay: read job status failed",
					"outbox_id", e.ID, "job_id", e.JobID, "error", err)
			}
			continue
		}

		switch status {
		case jobqueue.StatusSucceeded:
			if err := r.repo.MarkDispatched(ctx, e.ID, r.clock()); err != nil {
				r.logger.Error("outbox relay: mark dispatched failed", "outbox_id", e.ID, "error", err)
				continue
			}
			r.metrics.IncDispatched(e.EventType)
			r.logger.Debug("outbox event delivered",
				"outbox_id", e.ID, "event_type", e.EventType, "dedupe_key", e.DedupeKey)
			resolved++
		case jobqueue.StatusDead:
			// Poison. Terminal here too: the queue already exhausted its
			// retry budget, and re-dispatching from the outbox would be the
			// second retry mechanism this design exists to avoid. Dead is
			// what unblocks the rest of the aggregate.
			r.deadLetter(ctx, e, fmt.Sprintf("delivery job %s dead-lettered", e.JobID))
			resolved++
		default:
			// pending or running: still the queue's problem. The aggregate
			// stays blocked, which is exactly the ordering guarantee.
		}
	}
	return resolved
}

// dispatch claims due aggregate heads and hands each to the job queue.
func (r *Relay) dispatch(ctx context.Context) int {
	claimed, err := r.repo.ClaimDue(ctx, ClaimParams{
		Limit: r.cfg.BatchSize,
		Lease: r.cfg.Lease,
		Now:   r.clock(),
	})
	if err != nil {
		if ctx.Err() == nil {
			r.logger.Error("outbox relay: claim failed", "error", err)
		}
		return 0
	}

	dispatched := 0
	for _, e := range claimed {
		jobType, ok := r.routes[e.EventType]
		if !ok {
			// Unroutable: no handler exists for this event type. Retrying
			// cannot conjure one, so this is poison by definition.
			r.deadLetter(ctx, e, fmt.Sprintf("no route registered for event type %q", e.EventType))
			continue
		}

		job, err := r.enqueue.Enqueue(ctx, jobqueue.EnqueueInput{
			Type:    jobType,
			Payload: e.Payload,
			// The dedupe key is the queue's idempotency key, so a
			// re-handed-over event joins the existing live job instead of
			// creating a second one. This is the "reuse the job queue's
			// idempotency machinery" requirement: attempt counting and
			// backoff for the delivery live there and only there.
			IdempotencyKey: e.DedupeKey,
			CorrelationID:  e.DedupeKey,
		})
		if err != nil {
			// The hand-off failed, not the delivery. Bounded by
			// MaxAttempts: an event that cannot even be enqueued after
			// that many tries is poison, and holding its aggregate open
			// indefinitely helps nobody.
			if e.Attempts >= e.MaxAttempts {
				r.deadLetter(ctx, e, fmt.Sprintf("enqueue failed after %d attempts: %v", e.Attempts, err))
				continue
			}
			r.release(ctx, e, err.Error())
			continue
		}

		if err := r.repo.MarkDispatching(ctx, e.ID, job.ID); err != nil {
			// The job exists and will run; we just failed to record which
			// one. The lease lapses, the event is reclaimed, and the
			// idempotency key above makes the re-enqueue return this same
			// job — so this is recoverable, not a lost side effect.
			r.logger.Error("outbox relay: record job id failed",
				"outbox_id", e.ID, "job_id", job.ID, "error", err)
			continue
		}
		r.metrics.IncRelayed(e.EventType)
		r.logger.Debug("outbox event handed to job queue",
			"outbox_id", e.ID, "event_type", e.EventType, "job_id", job.ID, "dedupe_key", e.DedupeKey)
		dispatched++
	}
	return dispatched
}

// deadLetter parks an event in the terminal poison state and alerts. It is
// logged at Error and counted so a stuck side effect is visible rather than
// quietly buried in a table nobody reads.
func (r *Relay) deadLetter(ctx context.Context, e Event, reason string) {
	if err := r.repo.MarkDead(ctx, e.ID, reason); err != nil {
		r.logger.Error("outbox relay: mark dead failed", "outbox_id", e.ID, "error", err)
		return
	}
	r.metrics.IncDeadLettered(e.EventType)
	r.logger.Error("outbox event dead-lettered",
		"outbox_id", e.ID,
		"event_type", e.EventType,
		"aggregate_type", e.AggregateType,
		"aggregate_id", e.AggregateID,
		"dedupe_key", e.DedupeKey,
		"attempts", e.Attempts,
		"reason", reason,
	)
}

// release returns an event to pending for another hand-off attempt.
func (r *Relay) release(ctx context.Context, e Event, reason string) {
	next := r.clock().Add(r.cfg.Backoff)
	if err := r.repo.Release(ctx, e.ID, next, reason); err != nil {
		r.logger.Error("outbox relay: release failed", "outbox_id", e.ID, "error", err)
		return
	}
	r.logger.Warn("outbox event returned to pending",
		"outbox_id", e.ID, "event_type", e.EventType, "attempts", e.Attempts, "reason", reason)
}

func (r *Relay) refreshStats(ctx context.Context) {
	s, err := r.repo.Stats(ctx, r.clock())
	if err != nil {
		if ctx.Err() == nil {
			r.logger.Error("outbox relay: stats refresh failed", "error", err)
		}
		return
	}
	r.metrics.SetPendingDepth(s.Pending)
	r.metrics.SetDispatchingDepth(s.Dispatching)
	r.metrics.SetDeadDepth(s.Dead)
	r.metrics.SetOldestPendingAge(s.OldestPendingAge)
}

// discardWriter is the io.Writer slog fallback when a nil logger is passed.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
