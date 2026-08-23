package outbox

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
)

// memRepo is an in-memory Repository that reproduces the Postgres
// implementation's contract — most importantly ClaimDue's one-event-per-
// aggregate head semantics, which is where the ordering guarantee lives.
// The relay's behaviour is testable without a database precisely because
// that contract is stated in the port rather than hidden in SQL.
type memRepo struct {
	mu     sync.Mutex
	events map[uuid.UUID]*Event
	seq    []uuid.UUID // insertion order, standing in for (created_at, id)

	// failInsert / failClaim let a test inject store failures.
	failClaim error
}

func newMemRepo() *memRepo {
	return &memRepo{events: map[uuid.UUID]*Event{}}
}

func (m *memRepo) Insert(_ context.Context, _ Execer, e Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.events {
		if existing.DedupeKey == e.DedupeKey {
			return nil // ON CONFLICT (dedupe_key) DO NOTHING
		}
	}
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.Status == "" {
		e.Status = StatusPending
	}
	if e.MaxAttempts <= 0 {
		e.MaxAttempts = DefaultMaxAttempts
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	copied := e
	m.events[e.ID] = &copied
	m.seq = append(m.seq, e.ID)
	return nil
}

// heads returns the oldest non-terminal event of each aggregate, in
// insertion order — the DISTINCT ON in the SQL implementation.
func (m *memRepo) heads() []*Event {
	seen := map[string]bool{}
	var out []*Event
	for _, id := range m.seq {
		e := m.events[id]
		if e == nil || (e.Status != StatusPending && e.Status != StatusDispatching) {
			continue
		}
		key := e.AggregateType + "\x00" + e.AggregateID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, e)
	}
	return out
}

func (m *memRepo) ClaimDue(_ context.Context, params ClaimParams) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failClaim != nil {
		return nil, m.failClaim
	}
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}

	var claimed []Event
	for _, e := range m.heads() {
		if len(claimed) >= params.Limit {
			break
		}
		// No attempts cap, matching the SQL: a row that reached its budget
		// must still be claimable so the relay can dead-letter it, or it
		// blocks its aggregate forever.
		due := e.Status == StatusPending && !e.NextAttemptAt.After(now)
		abandoned := e.Status == StatusDispatching && e.JobID == nil &&
			e.LeasedUntil != nil && !e.LeasedUntil.After(now)
		if !due && !abandoned {
			continue
		}
		e.Status = StatusDispatching
		e.JobID = nil
		lease := now.Add(params.Lease)
		e.LeasedUntil = &lease
		e.Attempts++
		claimed = append(claimed, *e)
	}
	return claimed, nil
}

func (m *memRepo) MarkDispatching(_ context.Context, id, jobID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.events[id]
	if !ok {
		return ErrNotFound
	}
	if e.Status != StatusDispatching {
		return nil
	}
	e.JobID = &jobID
	return nil
}

func (m *memRepo) MarkDispatched(_ context.Context, id uuid.UUID, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.events[id]
	if !ok {
		return ErrNotFound
	}
	e.Status = StatusDispatched
	e.DispatchedAt = &at
	e.LeasedUntil = nil
	return nil
}

func (m *memRepo) MarkDead(_ context.Context, id uuid.UUID, lastErr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.events[id]
	if !ok {
		return ErrNotFound
	}
	e.Status = StatusDead
	e.LastError = lastErr
	e.LeasedUntil = nil
	return nil
}

func (m *memRepo) Release(_ context.Context, id uuid.UUID, nextAttemptAt time.Time, lastErr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.events[id]
	if !ok {
		return ErrNotFound
	}
	if e.Status != StatusDispatching {
		return nil
	}
	e.Status = StatusPending
	e.NextAttemptAt = nextAttemptAt
	e.LastError = lastErr
	e.LeasedUntil = nil
	e.JobID = nil
	return nil
}

func (m *memRepo) InFlight(_ context.Context, limit int) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Event
	for _, id := range m.seq {
		if len(out) >= limit {
			break
		}
		e := m.events[id]
		if e != nil && e.Status == StatusDispatching && e.JobID != nil {
			out = append(out, *e)
		}
	}
	return out, nil
}

func (m *memRepo) Stats(_ context.Context, now time.Time) (Stats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var s Stats
	var oldest time.Time
	for _, e := range m.events {
		switch e.Status {
		case StatusPending:
			if !e.NextAttemptAt.After(now) {
				s.Pending++
			}
		case StatusDispatching:
			s.Dispatching++
		case StatusDead:
			s.Dead++
		}
		if e.Status == StatusPending || e.Status == StatusDispatching {
			if oldest.IsZero() || e.CreatedAt.Before(oldest) {
				oldest = e.CreatedAt
			}
		}
	}
	if !oldest.IsZero() && now.After(oldest) {
		s.OldestPendingAge = now.Sub(oldest)
	}
	return s, nil
}

func (m *memRepo) PruneTerminal(_ context.Context, dispatchedBefore, deadBefore time.Time) (int64, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var dispatched, dead int64
	kept := m.seq[:0]
	for _, id := range m.seq {
		e := m.events[id]
		switch {
		case e.Status == StatusDispatched && e.UpdatedAt.Before(dispatchedBefore):
			delete(m.events, id)
			dispatched++
		case e.Status == StatusDead && e.UpdatedAt.Before(deadBefore):
			delete(m.events, id)
			dead++
		default:
			kept = append(kept, id)
		}
	}
	m.seq = kept
	return dispatched, dead, nil
}

// byID returns a copy of the stored event, for assertions.
func (m *memRepo) byID(id uuid.UUID) Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.events[id]; ok {
		return *e
	}
	return Event{}
}

// byDedupeKey looks an event up the way a consumer would identify it.
func (m *memRepo) byDedupeKey(key string) (Event, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.events {
		if e.DedupeKey == key {
			return *e, true
		}
	}
	return Event{}, false
}

// memQueue is an in-memory stand-in for the durable job queue: it records
// enqueues, deduplicates on (type, idempotency key) while a job is live, and
// lets a test drive each job to a terminal state.
type memQueue struct {
	mu       sync.Mutex
	jobs     map[uuid.UUID]*jobqueue.Job
	byKey    map[string]uuid.UUID
	order    []uuid.UUID
	failWith error
}

func newMemQueue() *memQueue {
	return &memQueue{
		jobs:  map[uuid.UUID]*jobqueue.Job{},
		byKey: map[string]uuid.UUID{},
	}
}

func (q *memQueue) Enqueue(_ context.Context, in jobqueue.EnqueueInput) (jobqueue.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.failWith != nil {
		return jobqueue.Job{}, q.failWith
	}
	key := in.Type + "\x00" + in.IdempotencyKey
	if in.IdempotencyKey != "" {
		if id, ok := q.byKey[key]; ok {
			existing := q.jobs[id]
			if existing.Status == jobqueue.StatusPending || existing.Status == jobqueue.StatusRunning {
				return *existing, nil
			}
		}
	}
	job := jobqueue.Job{
		ID:             uuid.New(),
		Type:           in.Type,
		Payload:        in.Payload,
		Status:         jobqueue.StatusPending,
		IdempotencyKey: in.IdempotencyKey,
	}
	q.jobs[job.ID] = &job
	q.order = append(q.order, job.ID)
	if in.IdempotencyKey != "" {
		q.byKey[key] = job.ID
	}
	return job, nil
}

func (q *memQueue) JobStatus(_ context.Context, id uuid.UUID) (jobqueue.Status, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	job, ok := q.jobs[id]
	if !ok {
		return "", jobqueue.ErrNotFound
	}
	return job.Status, nil
}

// remove deletes a job row outright, as a retention sweep or an operator
// would. It disappears from the idempotency index too, which is what makes a
// re-enqueue create a fresh job rather than join the vanished one.
func (q *memQueue) remove(id uuid.UUID) {
	q.mu.Lock()
	defer q.mu.Unlock()
	job, ok := q.jobs[id]
	if !ok {
		return
	}
	delete(q.jobs, id)
	if job.IdempotencyKey != "" {
		delete(q.byKey, job.Type+"\x00"+job.IdempotencyKey)
	}
}

// finish drives every live job to status. Returns how many it moved.
func (q *memQueue) finish(status jobqueue.Status) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, id := range q.order {
		job := q.jobs[id]
		if job.Status == jobqueue.StatusPending || job.Status == jobqueue.StatusRunning {
			job.Status = status
			n++
		}
	}
	return n
}

// finishType drives every live job of one type to status.
func (q *memQueue) finishType(jobType string, status jobqueue.Status) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, id := range q.order {
		job := q.jobs[id]
		if job.Type != jobType {
			continue
		}
		if job.Status == jobqueue.StatusPending || job.Status == jobqueue.StatusRunning {
			job.Status = status
			n++
		}
	}
	return n
}

// payloads returns every enqueued payload, in enqueue order.
func (q *memQueue) payloads() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]string, 0, len(q.order))
	for _, id := range q.order {
		out = append(out, string(q.jobs[id].Payload))
	}
	return out
}

// idempotencyKeys returns every enqueued job's idempotency key, sorted.
func (q *memQueue) idempotencyKeys() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]string, 0, len(q.order))
	for _, id := range q.order {
		out = append(out, q.jobs[id].IdempotencyKey)
	}
	sort.Strings(out)
	return out
}

func (q *memQueue) count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.order)
}

var errQueueDown = errors.New("queue unavailable")
