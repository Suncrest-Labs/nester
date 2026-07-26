package jobqueue

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// memRepo is an in-memory Repository that mirrors the Postgres semantics
// (lease-based dequeue, attempt increment, idempotent enqueue, lease-guarded
// terminal writes) so the worker pool can be unit-tested without a database.
type memRepo struct {
	mu   sync.Mutex
	jobs map[uuid.UUID]*Job
}

func newMemRepo() *memRepo { return &memRepo{jobs: map[uuid.UUID]*Job{}} }

func (m *memRepo) Enqueue(_ context.Context, in EnqueueInput) (Job, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if in.IdempotencyKey != "" {
		for _, j := range m.jobs {
			if j.Type == in.Type && j.IdempotencyKey == in.IdempotencyKey &&
				(j.Status == StatusPending || j.Status == StatusRunning) {
				return *j, false, nil
			}
		}
	}
	maxAttempts := in.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxAttempts
	}
	runAt := in.RunAt
	if runAt.IsZero() {
		runAt = time.Now()
	}
	payload := in.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	j := &Job{
		ID:             uuid.New(),
		Type:           in.Type,
		Payload:        payload,
		Status:         StatusPending,
		Priority:       in.Priority,
		MaxAttempts:    maxAttempts,
		IdempotencyKey: in.IdempotencyKey,
		CorrelationID:  in.CorrelationID,
		NextRunAt:      runAt,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	m.jobs[j.ID] = j
	return *j, true, nil
}

func (m *memRepo) Dequeue(_ context.Context, p DequeueParams) ([]Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var ready []*Job
	for _, j := range m.jobs {
		if j.Type != p.Type {
			continue
		}
		runnable := (j.Status == StatusPending && !j.NextRunAt.After(p.Now)) ||
			(j.Status == StatusRunning && j.LeasedUntil != nil && !j.LeasedUntil.After(p.Now))
		if runnable {
			ready = append(ready, j)
		}
	}
	sort.Slice(ready, func(a, b int) bool {
		if ready[a].Priority != ready[b].Priority {
			return ready[a].Priority > ready[b].Priority
		}
		return ready[a].NextRunAt.Before(ready[b].NextRunAt)
	})
	if len(ready) > p.Limit {
		ready = ready[:p.Limit]
	}
	leased := p.Now.Add(p.Lease)
	out := make([]Job, 0, len(ready))
	for _, j := range ready {
		j.Status = StatusRunning
		lu := leased
		j.LeasedUntil = &lu
		j.Attempts++
		j.UpdatedAt = time.Now()
		out = append(out, *j)
	}
	return out, nil
}

func (m *memRepo) Complete(_ context.Context, id uuid.UUID, result json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[id]; ok && j.Status == StatusRunning {
		j.Status = StatusSucceeded
		j.Result = result
		j.LeasedUntil = nil
		j.UpdatedAt = time.Now()
	}
	return nil
}

func (m *memRepo) Retry(_ context.Context, id uuid.UUID, nextRunAt time.Time, lastErr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[id]; ok && j.Status == StatusRunning {
		j.Status = StatusPending
		j.NextRunAt = nextRunAt
		j.LastError = lastErr
		j.LeasedUntil = nil
		j.UpdatedAt = time.Now()
	}
	return nil
}

func (m *memRepo) DeadLetter(_ context.Context, id uuid.UUID, lastErr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[id]; ok && j.Status == StatusRunning {
		j.Status = StatusDead
		j.LastError = lastErr
		j.LeasedUntil = nil
		j.UpdatedAt = time.Now()
	}
	return nil
}

func (m *memRepo) Heartbeat(_ context.Context, id uuid.UUID, leasedUntil time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok || j.Status != StatusRunning {
		return ErrNotFound
	}
	j.LeasedUntil = &leasedUntil
	return nil
}

func (m *memRepo) Stats(_ context.Context, now time.Time) (Stats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var s Stats
	s.DepthByType = map[string]int64{}
	for _, j := range m.jobs {
		switch j.Status {
		case StatusPending:
			if j.NextRunAt.After(now) {
				s.Scheduled++
			} else {
				s.Depth++
			}
			s.DepthByType[j.Type]++
		case StatusRunning:
			s.Running++
			s.DepthByType[j.Type]++
		case StatusDead:
			s.DeadLetter++
		}
	}
	return s, nil
}

func (m *memRepo) get(id uuid.UUID) Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *m.jobs[id]
}

func (m *memRepo) count(status Status) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, j := range m.jobs {
		if j.Status == status {
			n++
		}
	}
	return n
}

// waitFor polls cond until true or the deadline, failing the test otherwise.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

func fastConfig() Config {
	return Config{
		Enabled:            true,
		PollInterval:       5 * time.Millisecond,
		Lease:              200 * time.Millisecond,
		HeartbeatInterval:  50 * time.Millisecond,
		DefaultConcurrency: 4,
		Backoff:            BackoffConfig{Base: time.Millisecond, Max: 10 * time.Millisecond},
		DrainTimeout:       2 * time.Second,
	}
}

func TestWorker_SucceedsAndMarksJob(t *testing.T) {
	repo := newMemRepo()
	var ran atomic.Int32
	w := NewWorker(repo, fastConfig(), nil, nil).
		Register("noop", HandlerFunc(func(context.Context, Job) error {
			ran.Add(1)
			return nil
		}), 0)

	job, _, _ := repo.Enqueue(context.Background(), EnqueueInput{Type: "noop"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	waitFor(t, time.Second, func() bool { return repo.get(job.ID).Status == StatusSucceeded })
	if ran.Load() != 1 {
		t.Fatalf("handler ran %d times, want 1", ran.Load())
	}
}

func TestWorker_RetriesThenSucceeds(t *testing.T) {
	repo := newMemRepo()
	var calls atomic.Int32
	w := NewWorker(repo, fastConfig(), nil, nil).
		Register("flaky", HandlerFunc(func(context.Context, Job) error {
			if calls.Add(1) < 3 {
				return errors.New("transient")
			}
			return nil
		}), 0)

	job, _, _ := repo.Enqueue(context.Background(), EnqueueInput{Type: "flaky"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	waitFor(t, 2*time.Second, func() bool { return repo.get(job.ID).Status == StatusSucceeded })
	final := repo.get(job.ID)
	if final.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", final.Attempts)
	}
}

func TestWorker_DeadLettersAfterMaxAttempts(t *testing.T) {
	repo := newMemRepo()
	w := NewWorker(repo, fastConfig(), nil, nil).
		Register("always-fail", HandlerFunc(func(context.Context, Job) error {
			return errors.New("boom")
		}), 0)

	job, _, _ := repo.Enqueue(context.Background(), EnqueueInput{Type: "always-fail", MaxAttempts: 3})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	waitFor(t, 2*time.Second, func() bool { return repo.get(job.ID).Status == StatusDead })
	final := repo.get(job.ID)
	if final.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", final.Attempts)
	}
	if final.LastError == "" {
		t.Fatal("expected last_error to be recorded on dead-letter")
	}
}

func TestWorker_PermanentErrorSkipsRetries(t *testing.T) {
	repo := newMemRepo()
	var calls atomic.Int32
	w := NewWorker(repo, fastConfig(), nil, nil).
		Register("poison", HandlerFunc(func(context.Context, Job) error {
			calls.Add(1)
			return Permanent(errors.New("malformed payload"))
		}), 0)

	job, _, _ := repo.Enqueue(context.Background(), EnqueueInput{Type: "poison", MaxAttempts: 10})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	waitFor(t, time.Second, func() bool { return repo.get(job.ID).Status == StatusDead })
	if got := calls.Load(); got != 1 {
		t.Fatalf("permanent-error handler ran %d times, want 1", got)
	}
}

func TestWorker_RespectsPerTypeConcurrency(t *testing.T) {
	repo := newMemRepo()
	const limit = 2
	var inFlight, maxSeen atomic.Int32
	release := make(chan struct{})

	w := NewWorker(repo, fastConfig(), nil, nil).
		Register("bounded", HandlerFunc(func(ctx context.Context, _ Job) error {
			cur := inFlight.Add(1)
			for {
				old := maxSeen.Load()
				if cur <= old || maxSeen.CompareAndSwap(old, cur) {
					break
				}
			}
			<-release
			inFlight.Add(-1)
			return nil
		}), limit)

	for i := 0; i < 6; i++ {
		_, _, _ = repo.Enqueue(context.Background(), EnqueueInput{Type: "bounded"})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	// Let the worker saturate its concurrency window, then release.
	waitFor(t, time.Second, func() bool { return inFlight.Load() == limit })
	time.Sleep(50 * time.Millisecond)
	if got := maxSeen.Load(); got > limit {
		t.Fatalf("max concurrent = %d, exceeds limit %d", got, limit)
	}
	close(release)

	waitFor(t, 2*time.Second, func() bool { return repo.count(StatusSucceeded) == 6 })
}

func TestWorker_CrashRecoveryReclaimsExpiredLease(t *testing.T) {
	repo := newMemRepo()

	// Simulate a job leased by a since-crashed worker: running, lease already
	// expired. A fresh worker must reclaim and complete it.
	past := time.Now().Add(-time.Hour)
	id := uuid.New()
	repo.jobs[id] = &Job{
		ID:          id,
		Type:        "recover",
		Payload:     json.RawMessage(`{}`),
		Status:      StatusRunning,
		MaxAttempts: 5,
		Attempts:    1,
		NextRunAt:   past,
		LeasedUntil: &past,
		CreatedAt:   past,
		UpdatedAt:   past,
	}

	var ran atomic.Int32
	w := NewWorker(repo, fastConfig(), nil, nil).
		Register("recover", HandlerFunc(func(context.Context, Job) error {
			ran.Add(1)
			return nil
		}), 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	waitFor(t, time.Second, func() bool { return repo.get(id).Status == StatusSucceeded })
	if ran.Load() != 1 {
		t.Fatalf("reclaimed job ran %d times, want 1", ran.Load())
	}
}

func TestWorker_IdempotentEnqueueDeduplicates(t *testing.T) {
	repo := newMemRepo()
	first, created1, _ := repo.Enqueue(context.Background(), EnqueueInput{
		Type: "harvest", IdempotencyKey: "vault-123",
	})
	second, created2, _ := repo.Enqueue(context.Background(), EnqueueInput{
		Type: "harvest", IdempotencyKey: "vault-123",
	})

	if !created1 {
		t.Fatal("first enqueue should report created=true")
	}
	if created2 {
		t.Fatal("second enqueue with same key should report created=false")
	}
	if first.ID != second.ID {
		t.Fatalf("dedupe failed: %s != %s", first.ID, second.ID)
	}
	if n := len(repo.jobs); n != 1 {
		t.Fatalf("expected 1 job after dedupe, got %d", n)
	}
}

func TestWorker_GracefulShutdownDrainsInFlight(t *testing.T) {
	repo := newMemRepo()
	started := make(chan struct{})
	var completed atomic.Bool

	w := NewWorker(repo, fastConfig(), nil, nil).
		Register("slow", HandlerFunc(func(ctx context.Context, _ Job) error {
			close(started)
			time.Sleep(120 * time.Millisecond) // outlives the cancel below
			completed.Store(true)
			return nil
		}), 0)

	job, _, _ := repo.Enqueue(context.Background(), EnqueueInput{Type: "slow"})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()

	<-started
	cancel() // request shutdown while the job is mid-flight

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after shutdown")
	}
	if !completed.Load() {
		t.Fatal("in-flight job was not allowed to finish during drain")
	}
	if repo.get(job.ID).Status != StatusSucceeded {
		t.Fatalf("drained job status = %s, want succeeded", repo.get(job.ID).Status)
	}
}

// staticRepo/compile check: memRepo satisfies Repository.
var _ Repository = (*memRepo)(nil)
