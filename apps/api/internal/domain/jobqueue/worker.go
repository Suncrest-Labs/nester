package jobqueue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Config controls the worker pool. Read once at construction.
type Config struct {
	// Enabled: when false, Run returns immediately.
	Enabled bool
	// PollInterval is how long to wait between polls when the last poll found
	// no runnable work. When a poll finds work, the next poll runs immediately.
	PollInterval time.Duration
	// Lease is the visibility timeout: a leased job is invisible to other
	// workers until Lease elapses (or it completes / is heartbeated).
	Lease time.Duration
	// HeartbeatInterval is how often an in-flight job's lease is extended.
	// Must be well under Lease. Zero disables heartbeating.
	HeartbeatInterval time.Duration
	// JobTimeout bounds a single handler execution. Zero means no timeout
	// beyond the lease.
	JobTimeout time.Duration
	// DefaultConcurrency is the per-type parallelism when a type registers
	// without an explicit limit. Minimum 1.
	DefaultConcurrency int
	// Backoff parameterizes retry scheduling.
	Backoff BackoffConfig
	// StatsInterval is how often queue-depth / DLQ-depth gauges are refreshed.
	// Zero disables periodic stats refresh.
	StatsInterval time.Duration
	// DrainTimeout bounds how long Run waits for in-flight jobs to finish after
	// its context is cancelled. Jobs still running when it elapses keep their
	// lease and are re-run later (safe because handlers are idempotent).
	DrainTimeout time.Duration
}

func (c Config) withDefaults() Config {
	if c.PollInterval <= 0 {
		c.PollInterval = time.Second
	}
	if c.Lease <= 0 {
		c.Lease = 30 * time.Second
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = c.Lease / 3
	}
	if c.DefaultConcurrency < 1 {
		c.DefaultConcurrency = 1
	}
	if c.DrainTimeout <= 0 {
		c.DrainTimeout = 30 * time.Second
	}
	return c
}

type registration struct {
	handler     Handler
	concurrency int
	sem         chan struct{}
}

// Worker is the long-lived pool that dequeues and executes jobs. Construct with
// NewWorker, Register handlers, then call Run from a goroutine.
type Worker struct {
	repo    Repository
	cfg     Config
	logger  *slog.Logger
	metrics Metrics

	mu   sync.Mutex // guards handlers before Run; read-only after
	regs map[string]*registration

	wg  sync.WaitGroup
	rng *rand.Rand
}

// NewWorker constructs a Worker. logger and metrics may be nil.
func NewWorker(repo Repository, cfg Config, logger *slog.Logger, metrics Metrics) *Worker {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	if metrics == nil {
		metrics = NoopMetrics{}
	}
	return &Worker{
		repo:    repo,
		cfg:     cfg.withDefaults(),
		logger:  logger,
		metrics: metrics,
		regs:    map[string]*registration{},
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())), //nolint:gosec // jitter only
	}
}

// Register associates a handler with a job type and its concurrency limit.
// concurrency <= 0 uses Config.DefaultConcurrency. Registering the same type
// twice panics — misconfiguration should fail loudly at startup. Must be called
// before Run.
func (w *Worker) Register(jobType string, handler Handler, concurrency int) *Worker {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.regs[jobType]; exists {
		panic(fmt.Sprintf("jobqueue: handler for type %q already registered", jobType))
	}
	if concurrency <= 0 {
		concurrency = w.cfg.DefaultConcurrency
	}
	w.regs[jobType] = &registration{
		handler:     handler,
		concurrency: concurrency,
		sem:         make(chan struct{}, concurrency),
	}
	return w
}

// Run drives the poll/dispatch loop until ctx is cancelled, then drains
// in-flight jobs up to Config.DrainTimeout. Returns nil on clean shutdown.
func (w *Worker) Run(ctx context.Context) error {
	if !w.cfg.Enabled {
		w.logger.Info("job queue worker disabled; not starting")
		return nil
	}
	if len(w.regs) == 0 {
		w.logger.Warn("job queue worker started with no registered handlers")
	}
	w.logger.Info("job queue worker starting",
		"types", len(w.regs),
		"poll_interval", w.cfg.PollInterval,
		"lease", w.cfg.Lease,
	)

	poll := time.NewTimer(0)
	defer poll.Stop()

	var stats *time.Ticker
	var statsC <-chan time.Time
	if w.cfg.StatsInterval > 0 {
		stats = time.NewTicker(w.cfg.StatsInterval)
		defer stats.Stop()
		statsC = stats.C
		w.refreshStats(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("job queue worker draining", "drain_timeout", w.cfg.DrainTimeout)
			w.drain()
			return nil
		case <-statsC:
			w.refreshStats(ctx)
		case <-poll.C:
			dispatched := w.tick(ctx)
			// Fast-poll while there is work; back off to PollInterval when idle.
			if dispatched > 0 {
				poll.Reset(0)
			} else {
				poll.Reset(w.cfg.PollInterval)
			}
		}
	}
}

// tick performs one poll across all registered types and dispatches leased
// jobs. Returns the number of jobs dispatched. Only this goroutine dequeues, so
// per-type free-slot accounting via the semaphores is race-free.
func (w *Worker) tick(ctx context.Context) int {
	dispatched := 0
	for jobType, reg := range w.regs {
		free := cap(reg.sem) - len(reg.sem)
		if free <= 0 {
			continue
		}
		jobs, err := w.repo.Dequeue(ctx, DequeueParams{
			Type:  jobType,
			Limit: free,
			Lease: w.cfg.Lease,
			Now:   time.Now(),
		})
		if err != nil {
			if ctx.Err() != nil {
				return dispatched
			}
			w.logger.Error("job queue dequeue failed", "type", jobType, "error", err)
			continue
		}
		for _, job := range jobs {
			reg.sem <- struct{}{} // guaranteed non-blocking: free was computed above
			w.wg.Add(1)
			go w.process(reg, job)
			dispatched++
		}
	}
	return dispatched
}

// process executes a single leased job under a lease-bounded context, keeps the
// lease alive via heartbeats, and records the outcome.
func (w *Worker) process(reg *registration, job Job) {
	defer w.wg.Done()
	defer func() { <-reg.sem }()

	// Detach from the poll-loop context so graceful shutdown does not instantly
	// cancel work in flight; bound instead by JobTimeout and the lease heartbeat.
	base := context.Background()
	var cancel context.CancelFunc
	if w.cfg.JobTimeout > 0 {
		base, cancel = context.WithTimeout(base, w.cfg.JobTimeout)
	} else {
		base, cancel = context.WithCancel(base)
	}
	defer cancel()

	if w.cfg.HeartbeatInterval > 0 {
		stopHB := w.startHeartbeat(base, cancel, job.ID)
		defer stopHB()
	}

	start := time.Now()
	err := w.runHandler(base, reg.handler, job)
	w.metrics.ObserveLatency(job.Type, time.Since(start))

	logger := w.logger.With("job_id", job.ID, "type", job.Type, "attempt", job.Attempts)
	if job.CorrelationID != "" {
		logger = logger.With("correlation_id", job.CorrelationID)
	}

	// Use a fresh, short-lived context for the terminal state write so a
	// cancelled/expired job context cannot prevent us recording the outcome.
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer writeCancel()

	if err == nil {
		if cErr := w.repo.Complete(writeCtx, job.ID, nil); cErr != nil {
			logger.Error("job queue: mark complete failed", "error", cErr)
			return
		}
		w.metrics.IncProcessed(job.Type, "succeeded")
		logger.Debug("job succeeded")
		return
	}

	permanent := IsPermanent(err)
	exhausted := job.Attempts >= job.MaxAttempts
	if permanent || exhausted {
		if dErr := w.repo.DeadLetter(writeCtx, job.ID, err.Error()); dErr != nil {
			logger.Error("job queue: dead-letter failed", "error", dErr)
			return
		}
		w.metrics.IncProcessed(job.Type, "dead")
		logger.Warn("job dead-lettered", "permanent", permanent, "error", err)
		return
	}

	delay := backoffDuration(w.cfg.Backoff, job.Attempts, w.rng)
	if rErr := w.repo.Retry(writeCtx, job.ID, time.Now().Add(delay), err.Error()); rErr != nil {
		logger.Error("job queue: reschedule failed", "error", rErr)
		return
	}
	w.metrics.IncProcessed(job.Type, "retried")
	logger.Info("job failed; scheduled retry", "delay", delay, "error", err)
}

// runHandler invokes the handler, converting a panic into an error so one bad
// job never takes down the worker.
func (w *Worker) runHandler(ctx context.Context, h Handler, job Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panicked: %v", r)
		}
	}()
	return h.Handle(ctx, job)
}

// startHeartbeat extends the job's lease periodically until the returned stop
// func is called. If the lease can no longer be extended (job no longer running
// — e.g. reclaimed by another worker), it cancels the job context to prevent
// duplicate concurrent execution.
func (w *Worker) startHeartbeat(ctx context.Context, cancel context.CancelFunc, id uuid.UUID) (stop func()) {
	done := make(chan struct{})
	var once sync.Once
	stopFn := func() { once.Do(func() { close(done) }) }

	go func() {
		ticker := time.NewTicker(w.cfg.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				hbCtx, hbCancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := w.repo.Heartbeat(hbCtx, id, time.Now().Add(w.cfg.Lease))
				hbCancel()
				if errors.Is(err, ErrNotFound) {
					w.logger.Warn("job queue: lease lost, cancelling job", "job_id", id)
					cancel()
					return
				}
				if err != nil {
					w.logger.Error("job queue: heartbeat failed", "job_id", id, "error", err)
				}
			}
		}
	}()
	return stopFn
}

// drain waits for in-flight jobs to finish, up to Config.DrainTimeout.
func (w *Worker) drain() {
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		w.logger.Info("job queue worker stopped cleanly")
	case <-time.After(w.cfg.DrainTimeout):
		w.logger.Warn("job queue worker drain timed out; in-flight jobs will be retried after lease expiry")
	}
}

func (w *Worker) refreshStats(ctx context.Context) {
	s, err := w.repo.Stats(ctx, time.Now())
	if err != nil {
		if ctx.Err() == nil {
			w.logger.Error("job queue: stats refresh failed", "error", err)
		}
		return
	}
	w.metrics.SetDepth(s.Depth)
	w.metrics.SetDeadLetterDepth(s.DeadLetter)
}

// discardWriter is the io.Writer slog fallback when NewWorker(..., nil, ...) is used.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
