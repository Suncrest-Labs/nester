package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Router provides replica-aware data access: explicit read and write paths,
// per-role connection pools, read-your-writes consistency via primary
// pinning, and automatic routing around unhealthy or lagging replicas.
//
// Routing is EXPLICIT, never inferred from SQL. Repository methods that only
// read call Read; anything that writes — or must observe the very latest
// state (transactions, authoritative balance reads, accounting decisions) —
// calls Write. The routing decision lives at the call site, where the
// correctness knowledge lives.
type Router struct {
	primary  *sql.DB
	replicas []*replica
	pinner   Pinner
	next     atomic.Uint64 // round-robin cursor over replicas
}

type replica struct {
	db *sql.DB

	mu        sync.RWMutex
	healthy   bool
	lag       time.Duration
	maxLag    time.Duration
	updatedAt time.Time
}

// eligible reports whether the replica may serve reads: healthy and within
// its lag budget. A replica that has fallen too far behind is temporarily
// removed rather than serving dangerously stale data.
func (r *replica) eligible() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.healthy && (r.maxLag <= 0 || r.lag <= r.maxLag)
}

// Pinner tracks which users are pinned to the primary for reads. After a
// user writes, their reads stay on the primary for a bounded window so they
// always see their own writes despite replica lag. Production wiring backs
// this with Redis (key per user, TTL = safe lag window); tests and
// single-instance deployments can use NewMemoryPinner.
type Pinner interface {
	// Pin records that userID wrote and must read from the primary until
	// the window elapses.
	Pin(ctx context.Context, userID string)
	// Pinned reports whether userID is inside the read-your-writes window.
	Pinned(ctx context.Context, userID string) bool
}

// RouterConfig wires the per-role pools.
type RouterConfig struct {
	// Primary is the write pool configuration. Its sizing must account for
	// writes plus consistency-pinned reads.
	Primary Config
	// Replicas are the read pool configurations, one per replica.
	Replicas []Config
	// MaxReplicaLag removes a replica from the read pool while its reported
	// lag exceeds this budget. Zero disables lag-based eviction.
	MaxReplicaLag time.Duration
	// Pinner implements the read-your-writes window. Required.
	Pinner Pinner
}

// ErrNoPinner guards against silently losing the consistency guarantee.
var ErrNoPinner = errors.New("db: RouterConfig.Pinner is required for read-your-writes consistency")

// OpenRouter opens the primary and every replica pool. Replica open errors
// are not fatal: the router starts with that replica marked unhealthy and
// reads fall back to the primary — degraded capacity, not an outage.
func OpenRouter(cfg RouterConfig) (*Router, error) {
	if cfg.Pinner == nil {
		return nil, ErrNoPinner
	}
	primary, err := Open(cfg.Primary)
	if err != nil {
		return nil, fmt.Errorf("db: open primary: %w", err)
	}

	r := &Router{primary: primary, pinner: cfg.Pinner}
	for i, rc := range cfg.Replicas {
		rep := &replica{maxLag: cfg.MaxReplicaLag}
		if rdb, err := Open(rc); err == nil {
			rep.db = rdb
			rep.healthy = true
		} else {
			// Leave unhealthy; health checks may recover it later.
			rep.healthy = false
			_ = i
		}
		r.replicas = append(r.replicas, rep)
	}
	return r, nil
}

// NewRouter assembles a Router from already-open pools (tests, custom
// wiring). Every replica starts healthy.
func NewRouter(primary *sql.DB, replicas []*sql.DB, pinner Pinner, maxLag time.Duration) (*Router, error) {
	if pinner == nil {
		return nil, ErrNoPinner
	}
	r := &Router{primary: primary, pinner: pinner}
	for _, rdb := range replicas {
		r.replicas = append(r.replicas, &replica{db: rdb, healthy: true, maxLag: maxLag})
	}
	return r, nil
}

// Write returns the primary pool. Writes, transactions, and reads that must
// observe authoritative current state (ledger balances, reconciliation)
// ALWAYS use this path, regardless of pinning.
func (r *Router) Write() *sql.DB {
	return r.primary
}

// Read returns the pool a read-only query for userID should use: the
// primary while the user is inside their read-your-writes window, otherwise
// a healthy replica (round-robin), otherwise the primary as fallback.
func (r *Router) Read(ctx context.Context, userID string) *sql.DB {
	if userID != "" && r.pinner.Pinned(ctx, userID) {
		return r.primary
	}
	return r.readAny()
}

// ReadAny returns a replica pool for reads with no user context (background
// aggregation, public data). Falls back to the primary when no replica is
// eligible.
func (r *Router) ReadAny() *sql.DB {
	return r.readAny()
}

func (r *Router) readAny() *sql.DB {
	n := len(r.replicas)
	if n == 0 {
		return r.primary
	}
	start := r.next.Add(1)
	for i := 0; i < n; i++ {
		rep := r.replicas[(int(start)+i)%n]
		if rep.eligible() && rep.db != nil {
			return rep.db
		}
	}
	// All replicas unavailable: degraded capacity on the primary beats an
	// outage.
	return r.primary
}

// NoteWrite records that userID performed a write: their subsequent reads
// are pinned to the primary for the consistency window. Call this after
// every user-attributable write.
func (r *Router) NoteWrite(ctx context.Context, userID string) {
	if userID != "" {
		r.pinner.Pin(ctx, userID)
	}
}

// BeginTx opens a transaction — always on the primary; a transaction
// spanning read and write connections would be incoherent.
func (r *Router) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return r.primary.BeginTx(ctx, opts)
}

// SetReplicaHealth marks replica i healthy or unhealthy (from the health
// monitor). Out-of-range indexes are ignored.
func (r *Router) SetReplicaHealth(i int, healthy bool) {
	if i < 0 || i >= len(r.replicas) {
		return
	}
	rep := r.replicas[i]
	rep.mu.Lock()
	rep.healthy = healthy
	rep.updatedAt = time.Now()
	rep.mu.Unlock()
}

// SetReplicaLag records replica i's measured replication lag; a replica over
// its budget is routed around until it catches up.
func (r *Router) SetReplicaLag(i int, lag time.Duration) {
	if i < 0 || i >= len(r.replicas) {
		return
	}
	rep := r.replicas[i]
	rep.mu.Lock()
	rep.lag = lag
	rep.updatedAt = time.Now()
	rep.mu.Unlock()
}

// Stats exposes pool utilisation per role for metrics: a saturating primary
// pool or a lagging replica are leading indicators of trouble.
func (r *Router) Stats() RouterStats {
	s := RouterStats{Primary: r.primary.Stats()}
	for _, rep := range r.replicas {
		rep.mu.RLock()
		rs := ReplicaStats{Healthy: rep.healthy, Lag: rep.lag}
		if rep.db != nil {
			rs.Pool = rep.db.Stats()
		}
		rep.mu.RUnlock()
		s.Replicas = append(s.Replicas, rs)
	}
	return s
}

// RouterStats aggregates per-role pool statistics.
type RouterStats struct {
	Primary  sql.DBStats
	Replicas []ReplicaStats
}

// ReplicaStats couples a replica's pool stats with its health state.
type ReplicaStats struct {
	Pool    sql.DBStats
	Healthy bool
	Lag     time.Duration
}

// Close drains every pool; used by graceful shutdown.
func (r *Router) Close() error {
	var firstErr error
	if err := r.primary.Close(); err != nil {
		firstErr = err
	}
	for _, rep := range r.replicas {
		if rep.db == nil {
			continue
		}
		if err := rep.db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ---------------------------------------------------------------------------
// In-memory pinner
// ---------------------------------------------------------------------------

// MemoryPinner implements Pinner with an in-process TTL map. Suitable for
// tests and single-instance deployments; multi-instance production should
// back Pinner with Redis so the pin follows the user across instances.
type MemoryPinner struct {
	window time.Duration
	now    func() time.Time

	mu   sync.Mutex
	pins map[string]time.Time
}

// NewMemoryPinner builds a pinner with the given read-your-writes window.
func NewMemoryPinner(window time.Duration) *MemoryPinner {
	return &MemoryPinner{window: window, now: time.Now, pins: make(map[string]time.Time)}
}

// Pin implements Pinner.
func (m *MemoryPinner) Pin(_ context.Context, userID string) {
	m.mu.Lock()
	m.pins[userID] = m.now().Add(m.window)
	m.mu.Unlock()
}

// Pinned implements Pinner.
func (m *MemoryPinner) Pinned(_ context.Context, userID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	until, ok := m.pins[userID]
	if !ok {
		return false
	}
	if m.now().After(until) {
		delete(m.pins, userID)
		return false
	}
	return true
}
