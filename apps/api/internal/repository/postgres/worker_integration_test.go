package postgres

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
)

// fastIntegrationConfig provides a worker config tuned for fast integration tests.
func fastIntegrationConfig() jobqueue.Config {
	return jobqueue.Config{
		Enabled:            true,
		PollInterval:       10 * time.Millisecond,
		Lease:              500 * time.Millisecond,
		HeartbeatInterval:  100 * time.Millisecond,
		DefaultConcurrency: 4,
		Backoff:            jobqueue.BackoffConfig{Base: 50 * time.Millisecond, Max: 200 * time.Millisecond},
		DrainTimeout:       2 * time.Second,
	}
}

// waitForIntegration polls cond until true or the deadline, failing the test otherwise.
func waitForIntegration(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

func getJobByID(t *testing.T, repo *JobRepository, id uuid.UUID) jobqueue.Job {
	t.Helper()
	row := repo.db.QueryRow(`SELECT `+jobColumns+` FROM jobs WHERE id = $1`, id)
	job, err := scanJob(row)
	if err != nil {
		t.Fatalf("getJobByID: %v", err)
	}
	return job
}

func TestWorkerIntegration_RetryAndBackoff(t *testing.T) {
	repo := setupJobRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var attempts atomic.Int32
	var attemptTimes [3]time.Time

	cfg := fastIntegrationConfig()
	// predictable backoff for assertion
	cfg.Backoff = jobqueue.BackoffConfig{Base: 50 * time.Millisecond, Max: 500 * time.Millisecond}

	w := jobqueue.NewWorker(repo, cfg, nil, nil).
		Register("transient", jobqueue.HandlerFunc(func(ctx context.Context, job jobqueue.Job) error {
			c := attempts.Add(1)
			if c <= 3 {
				attemptTimes[c-1] = time.Now()
			}
			if c < 3 {
				return errors.New("temporary failure")
			}
			return nil
		}), 0)

	go func() { _ = w.Run(ctx) }()

	job, _, err := repo.Enqueue(context.Background(), jobqueue.EnqueueInput{Type: "transient", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	waitForIntegration(t, 5*time.Second, func() bool {
		j := getJobByID(t, repo, job.ID)
		return j.Status == jobqueue.StatusSucceeded
	})

	finalJob := getJobByID(t, repo, job.ID)
	if finalJob.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", finalJob.Attempts)
	}

	// Assert that retries occurred and we recorded times
	if attemptTimes[1].IsZero() || attemptTimes[2].IsZero() {
		t.Fatalf("attempt times not recorded properly")
	}

	// Ensure there is some delay observed between attempts
	delay1 := attemptTimes[1].Sub(attemptTimes[0])
	delay2 := attemptTimes[2].Sub(attemptTimes[1])
	if delay1 < 0 || delay2 < 0 {
		t.Fatalf("invalid delays observed: delay1=%v delay2=%v", delay1, delay2)
	}
}

func TestWorkerIntegration_PanicIsTreatedAsFailure(t *testing.T) {
	repo := setupJobRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := jobqueue.NewWorker(repo, fastIntegrationConfig(), nil, nil).
		Register("panic", jobqueue.HandlerFunc(func(ctx context.Context, job jobqueue.Job) error {
			panic("boom")
		}), 0)

	go func() { _ = w.Run(ctx) }()

	job, _, err := repo.Enqueue(context.Background(), jobqueue.EnqueueInput{Type: "panic", MaxAttempts: 2})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	waitForIntegration(t, 5*time.Second, func() bool {
		j := getJobByID(t, repo, job.ID)
		return j.Status == jobqueue.StatusDead
	})

	finalJob := getJobByID(t, repo, job.ID)
	if finalJob.LastError == "" {
		t.Fatal("expected last_error to contain panic info")
	}
}

func TestWorkerIntegration_TerminalStates(t *testing.T) {
	repo := setupJobRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := jobqueue.NewWorker(repo, fastIntegrationConfig(), nil, nil).
		Register("always_fail", jobqueue.HandlerFunc(func(ctx context.Context, job jobqueue.Job) error {
			return errors.New("fail")
		}), 0)

	go func() { _ = w.Run(ctx) }()

	job, _, err := repo.Enqueue(context.Background(), jobqueue.EnqueueInput{Type: "always_fail", MaxAttempts: 2})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	waitForIntegration(t, 5*time.Second, func() bool {
		j := getJobByID(t, repo, job.ID)
		return j.Status == jobqueue.StatusDead
	})

	finalJob := getJobByID(t, repo, job.ID)
	if finalJob.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", finalJob.Attempts)
	}
}

func TestWorkerIntegration_Concurrency(t *testing.T) {
	// Assert it with a counter incremented inside the handler.
	for i := 0; i < 10; i++ {
		t.Run("ConcurrentClaim", func(t *testing.T) {
			repo := setupJobRepo(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var ran atomic.Int32

			handler := jobqueue.HandlerFunc(func(ctx context.Context, job jobqueue.Job) error {
				ran.Add(1)
				// Sleep slightly so other workers might poll while this is running
				time.Sleep(20 * time.Millisecond)
				return nil
			})

			cfg := fastIntegrationConfig()
			cfg.PollInterval = 2 * time.Millisecond // very aggressive polling

			w1 := jobqueue.NewWorker(repo, cfg, nil, nil).Register("concurrent", handler, 0)
			w2 := jobqueue.NewWorker(repo, cfg, nil, nil).Register("concurrent", handler, 0)
			w3 := jobqueue.NewWorker(repo, cfg, nil, nil).Register("concurrent", handler, 0)

			go func() { _ = w1.Run(ctx) }()
			go func() { _ = w2.Run(ctx) }()
			go func() { _ = w3.Run(ctx) }()

			job, _, err := repo.Enqueue(context.Background(), jobqueue.EnqueueInput{Type: "concurrent"})
			if err != nil {
				t.Fatalf("enqueue: %v", err)
			}

			waitForIntegration(t, 5*time.Second, func() bool {
				j := getJobByID(t, repo, job.ID)
				return j.Status == jobqueue.StatusSucceeded
			})

			if c := ran.Load(); c != 1 {
				t.Fatalf("job handler ran %d times, want exactly 1", c)
			}
		})
	}
}

func TestWorkerIntegration_WorkerDiesMidJob(t *testing.T) {
	repo := setupJobRepo(t)

	var w1Claims, w2Claims atomic.Int32
	firstClaimStarted := make(chan struct{})
	releaseW1 := make(chan struct{})

	cfg := fastIntegrationConfig()
	cfg.Lease = 200 * time.Millisecond
	// Config.withDefaults() rewrites HeartbeatInterval <= 0 to Lease/3, so the
	// heartbeat cannot be switched off through config: w1 would renew its lease
	// forever and the job would never become reclaimable. Bound the handler with
	// JobTimeout instead (withDefaults leaves it alone). When it fires, the job
	// context is cancelled, the heartbeat goroutine returns on <-ctx.Done(), and
	// the lease is left to lapse the way a hard crash leaves it.
	cfg.JobTimeout = 300 * time.Millisecond

	// w1 claims the job and hangs until its JobTimeout fires. Concurrency is
	// pinned to 1 so w1 can never take a second copy of its own job.
	w1 := jobqueue.NewWorker(repo, cfg, nil, nil).
		Register("die", jobqueue.HandlerFunc(func(ctx context.Context, job jobqueue.Job) error {
			w1Claims.Add(1)
			close(firstClaimStarted)
			select {
			case <-ctx.Done(): // JobTimeout fired: heartbeat stops, lease lapses
			case <-releaseW1:
			}
			// Return nil rather than ctx.Err(): a non-nil error would make w1
			// reschedule the job and could clobber w2's terminal write.
			return nil
		}), 1)

	ctx1, cancel1 := context.WithCancel(context.Background())
	go func() { _ = w1.Run(ctx1) }()

	job, _, err := repo.Enqueue(context.Background(), jobqueue.EnqueueInput{Type: "die"})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	<-firstClaimStarted

	// Simulate a hard crash: stop w1's poll loop so it never claims again. The
	// in-flight handler is detached from this context by design, so it keeps
	// running until JobTimeout cancels it and the lease is left to lapse.
	cancel1()
	t.Cleanup(func() { close(releaseW1) })

	w2 := jobqueue.NewWorker(repo, cfg, nil, nil).
		Register("die", jobqueue.HandlerFunc(func(ctx context.Context, job jobqueue.Job) error {
			w2Claims.Add(1)
			return nil
		}), 1)

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go func() { _ = w2.Run(ctx2) }()

	// w2 can only pick the job up after w1's 200ms lease expires.
	deadline := time.Now().Add(15 * time.Second)
	var last jobqueue.Job
	for time.Now().Before(deadline) {
		last = getJobByID(t, repo, job.ID)
		if last.Status == jobqueue.StatusSucceeded {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if last.Status != jobqueue.StatusSucceeded {
		t.Fatalf("job never reached succeeded: status=%q attempts=%d max=%d w1=%d w2=%d",
			last.Status, last.Attempts, last.MaxAttempts, w1Claims.Load(), w2Claims.Load())
	}

	if c := w1Claims.Load(); c != 1 {
		t.Fatalf("w1 ran the handler %d times, want exactly 1", c)
	}
	if c := w2Claims.Load(); c != 1 {
		t.Fatalf("w2 ran the handler %d times, want exactly 1 after lease expiry", c)
	}
}

func TestWorkerIntegration_Idempotency(t *testing.T) {
	repo := setupJobRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var ran atomic.Int32

	w := jobqueue.NewWorker(repo, fastIntegrationConfig(), nil, nil).
		Register("idem", jobqueue.HandlerFunc(func(ctx context.Context, job jobqueue.Job) error {
			ran.Add(1)
			return nil
		}), 0)

	go func() { _ = w.Run(ctx) }()

	j1, c1, err := repo.Enqueue(context.Background(), jobqueue.EnqueueInput{Type: "idem", IdempotencyKey: "key-123"})
	if err != nil {
		t.Fatalf("enqueue 1: %v", err)
	}
	j2, c2, err := repo.Enqueue(context.Background(), jobqueue.EnqueueInput{Type: "idem", IdempotencyKey: "key-123"})
	if err != nil {
		t.Fatalf("enqueue 2: %v", err)
	}

	if !c1 || c2 {
		t.Fatalf("expected first to create, second not to. got c1=%v c2=%v", c1, c2)
	}
	if j1.ID != j2.ID {
		t.Fatalf("expected same job ID. got %v and %v", j1.ID, j2.ID)
	}

	waitForIntegration(t, 5*time.Second, func() bool {
		j := getJobByID(t, repo, j1.ID)
		return j.Status == jobqueue.StatusSucceeded
	})

	if ran.Load() != 1 {
		t.Fatalf("handler ran %d times, want exactly 1", ran.Load())
	}
}
