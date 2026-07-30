// Package scheduler: leader election (issue #846).
//
// Nester runs five independent background job loops (this file's package
// comment on the other files documents each). With more than one API
// replica, every replica runs every loop, which is wasteful for
// notification jobs and unsafe for money-moving ones (duplicate on-chain
// rebalance submissions, duplicate ledger deposits). Leadership elects a
// single leader instance per deployment so singleton job loops gate their
// tick-work behind "am I the leader right now" and only one replica ever
// acts.
package scheduler

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DefaultLeaderLockKey is the advisory-lock key used to elect one leader
// instance for the whole scheduler subsystem.
//
// Design choice: ONE lock key shared by all five job loops, not a key per
// job type.
//
//   - The issue frames this as "singleton jobs run on exactly one instance"
//     — a single leader concept, not five independently-elected leaders
//     that could legally land on five different replicas at once.
//   - One key means one failover path to reason about, log, and test — a
//     replica is either "the leader" or "a follower," full stop.
//   - Tradeoff: every singleton tick (rebalance evaluation, recurring
//     deposits, notification sweeps) executes on the SAME instance, so that
//     instance carries all of the scheduler subsystem's load rather than it
//     being spread across replicas. If that ever becomes a bottleneck, a
//     per-job-type lock key (e.g. hashing "scheduler:rebalance" vs
//     "scheduler:recurring-deposit" to distinct int64 keys) is a compatible
//     follow-up: IsLeader() call sites in the job loops don't change, only
//     how many Leadership instances main.go constructs and which one each
//     job is wired to.
const DefaultLeaderLockKey int64 = 846000

// defaultHeartbeatInterval is how often a follower attempts to acquire the
// lock and a leader reverifies it still holds it. This interval is also the
// upper bound on failover latency after a leader disappears.
const defaultHeartbeatInterval = 3 * time.Second

// LeadershipConfig controls the advisory-lock election loop. All instances
// in a deployment must agree on LockKey.
type LeadershipConfig struct {
	// LockKey is the pg_try_advisory_lock key shared by every instance.
	LockKey int64
	// HeartbeatInterval is how often a follower retries acquisition and a
	// leader reverifies its lock. Also bounds failover time.
	HeartbeatInterval time.Duration
	// InstanceID identifies this process in logs and /admin/health. Empty
	// generates hostname + a random UUID.
	InstanceID string
}

func (c LeadershipConfig) withDefaults() LeadershipConfig {
	if c.LockKey == 0 {
		c.LockKey = DefaultLeaderLockKey
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = defaultHeartbeatInterval
	}
	if c.InstanceID == "" {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "unknown-host"
		}
		c.InstanceID = host + "-" + uuid.NewString()
	}
	return c
}

// LeaderChecker is the narrow interface job loops depend on so they can gate
// tick-work behind current leadership. *Leadership implements it; tests
// pass a fake. A nil LeaderChecker on a job means "always leader" — kept
// backwards compatible with single-instance deployments and existing tests
// that construct jobs without wiring leadership at all.
type LeaderChecker interface {
	IsLeader() bool
}

// Leadership implements singleton-execution leader election over a single
// Postgres session-scoped advisory lock.
//
// # Failover semantics
//
// pg_advisory_lock / pg_try_advisory_lock locks are held by a SESSION, not a
// row with a TTL. Leadership pins one physical connection (*sql.Conn, via
// database/sql's DB.Conn) for the entire duration it holds the lock — this
// codebase's production DB access is exclusively database/sql (main.go does
// `db := stdlib.OpenDBFromPool(pgPool.Pool)`, and every repository takes a
// *sql.DB), so Leadership reuses that same pool rather than introducing a
// second, pgxpool-based connection-management strategy.
//
// If the pinned connection dies for any reason — process crash, OOM-kill,
// network partition, the pool evicting a broken connection — Postgres tears
// down that backend session and the advisory lock releases automatically.
// No heartbeat write, TTL expiry, or explicit unlock is required for the
// lock to become available again. Other instances only discover this
// through their own next acquisition attempt, which is why every instance
// polls pg_try_advisory_lock on HeartbeatInterval: that interval is the
// upper bound on how long it takes a follower to notice and become leader.
//
// # Split-brain guard
//
// Because leadership status is refreshed only once per HeartbeatInterval,
// IsLeader() can return a cached "yes" for up to that long after the lock
// was actually lost. Job loops MUST call IsLeader() again immediately
// before their money-moving or notification-sending step actually runs —
// not just once at the top of a tick — so a tick that starts as leader but
// loses the lock partway through still stops before acting. Combined with
// idempotent handlers (job-queue idempotency keys for money-moving work),
// this is the belt-and-suspenders defense against double-processing.
type Leadership struct {
	db     *sql.DB
	cfg    LeadershipConfig
	logger *slog.Logger

	mu       sync.RWMutex
	isLeader bool
	since    time.Time
	conn     *sql.Conn
}

// NewLeadership constructs a Leadership component. logger may be nil.
func NewLeadership(db *sql.DB, cfg LeadershipConfig, logger *slog.Logger) *Leadership {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	cfg = cfg.withDefaults()
	return &Leadership{
		db:     db,
		cfg:    cfg,
		logger: logger.With("instance_id", cfg.InstanceID, "lock_key", cfg.LockKey),
	}
}

// InstanceID returns this process's identity, as logged on every transition
// and reported at GET /api/v1/admin/health.
func (l *Leadership) InstanceID() string { return l.cfg.InstanceID }

// IsLeader reports whether this instance currently believes it holds
// leadership. Cheap: reads a mutex-protected bool refreshed on every
// HeartbeatInterval tick. Callers on singleton job loops MUST call this
// immediately before their tick-work actually acts (not only once at the
// top of the tick) — see the split-brain guard note on the type doc.
func (l *Leadership) IsLeader() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.isLeader
}

// Since returns when the current leadership term began, or the zero value
// when this instance is not currently leader.
func (l *Leadership) Since() time.Time {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if !l.isLeader {
		return time.Time{}
	}
	return l.since
}

// Run drives the election loop until ctx is cancelled. On cancellation it
// releases the lock (if held) and closes the pinned connection before
// returning, so callers can rely on release completing as part of graceful
// shutdown rather than only on eventual process exit severing the TCP
// connection.
func (l *Leadership) Run(ctx context.Context) {
	l.logger.Info("leadership election starting", "heartbeat_interval", l.cfg.HeartbeatInterval)

	ticker := time.NewTicker(l.cfg.HeartbeatInterval)
	defer ticker.Stop()

	l.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			// Use a fresh context: ctx is already cancelled, but the release
			// query still needs to reach Postgres.
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			l.release(releaseCtx)
			cancel()
			l.logger.Info("leadership election stopped")
			return
		case <-ticker.C:
			l.tick(ctx)
		}
	}
}

// tick performs one acquisition attempt (if follower) or reverification (if
// leader).
func (l *Leadership) tick(ctx context.Context) {
	l.mu.RLock()
	conn := l.conn
	l.mu.RUnlock()

	if conn == nil {
		l.acquire(ctx)
		return
	}

	// Already leader: reverify the pinned session is alive and still holds
	// the lock. pg_try_advisory_lock is reentrant within the same session:
	// calling it again while we already hold it just reconfirms success
	// without blocking. A dead connection makes the query itself fail,
	// which is exactly the "lock lost" signal we need.
	var acquired bool
	err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", l.cfg.LockKey).Scan(&acquired)
	if err != nil || !acquired {
		l.logger.Warn("leadership lost", "error", err)
		l.demote()
	}
}

func (l *Leadership) acquire(ctx context.Context) {
	conn, err := l.db.Conn(ctx)
	if err != nil {
		l.logger.Debug("leadership: acquire connection failed", "error", err)
		return
	}

	var acquired bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", l.cfg.LockKey).Scan(&acquired); err != nil {
		l.logger.Debug("leadership: try-lock query failed", "error", err)
		_ = conn.Close()
		return
	}
	if !acquired {
		_ = conn.Close()
		return
	}

	l.mu.Lock()
	l.conn = conn
	l.isLeader = true
	l.since = time.Now().UTC()
	since := l.since
	l.mu.Unlock()

	l.logger.Info("became leader", "since", since)
}

// demote clears leadership state after the pinned connection/lock is found
// to be gone. Closing the (already broken) connection is best-effort.
func (l *Leadership) demote() {
	l.mu.Lock()
	conn := l.conn
	wasLeader := l.isLeader
	l.conn = nil
	l.isLeader = false
	l.mu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
	if wasLeader {
		l.logger.Warn("demoted from leader to follower")
	}
}

// release unlocks (best-effort) and closes the pinned connection as part of
// graceful shutdown. Safe to call when not leader.
func (l *Leadership) release(ctx context.Context) {
	l.mu.Lock()
	conn := l.conn
	wasLeader := l.isLeader
	l.conn = nil
	l.isLeader = false
	l.mu.Unlock()

	if conn == nil {
		return
	}
	if wasLeader {
		// pg_advisory_unlock_all releases every advisory lock this session
		// holds regardless of how many times pg_try_advisory_lock was
		// called while reverifying, so we don't need to track a lock depth
		// counter. Closing the connection would release it anyway (session
		// end), but releasing explicitly lets the next leader take over
		// without waiting for the pool to notice the connection closed.
		if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_unlock_all()"); err != nil {
			l.logger.Warn("leadership: explicit unlock failed; connection close will release it anyway", "error", err)
		} else {
			l.logger.Info("released leadership")
		}
	}
	_ = conn.Close()
}
