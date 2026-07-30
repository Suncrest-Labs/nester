package scheduler

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// openLeadershipTestDB opens a real Postgres connection for advisory-lock
// tests. Advisory locks are a Postgres server-side primitive that cannot be
// faked, so — following the convention already established in
// internal/repository/postgres/job_repository_integration_test.go — these
// tests are skipped outright when TEST_DATABASE_DSN is unset rather than
// using testcontainers (not used anywhere in this repo).
func openLeadershipTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	// Advisory-lock tests pin one *sql.Conn per Leadership instance, plus
	// helper connections for backend-pid introspection; make sure the pool
	// can hand out enough distinct sessions concurrently.
	db.SetMaxOpenConns(10)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func testLogger(t *testing.T, name string) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})).With("component", name)
}

// awaitLeader polls until pred() is true or the timeout elapses, checking
// every 50ms — well under the 3s default HeartbeatInterval multiplied by
// the small multiple these tests wait for.
func awaitLeader(t *testing.T, timeout time.Duration, pred func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pred() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return pred()
}

// TestLeadership_SingleLeaderAmongTwoInstances is required test #1: two
// "instances" (two Leadership values sharing the same lock key against the
// same Postgres database — the shared lock backend two real replicas would
// point at) must elect exactly one leader.
func TestLeadership_SingleLeaderAmongTwoInstances(t *testing.T) {
	db := openLeadershipTestDB(t)
	lockKey := uniqueLockKey(t)

	cfgA := LeadershipConfig{LockKey: lockKey, HeartbeatInterval: 200 * time.Millisecond, InstanceID: "instance-a"}
	cfgB := LeadershipConfig{LockKey: lockKey, HeartbeatInterval: 200 * time.Millisecond, InstanceID: "instance-b"}

	leaderA := NewLeadership(db, cfgA, testLogger(t, "a"))
	leaderB := NewLeadership(db, cfgB, testLogger(t, "b"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go leaderA.Run(ctx)
	go leaderB.Run(ctx)

	ok := awaitLeader(t, 5*time.Second, func() bool {
		return leaderA.IsLeader() || leaderB.IsLeader()
	})
	if !ok {
		t.Fatal("neither instance became leader within timeout")
	}

	// Give the loser's acquisition attempts a few more heartbeats to prove
	// it never flips to leader too (not just "not yet").
	time.Sleep(1 * time.Second)

	if leaderA.IsLeader() && leaderB.IsLeader() {
		t.Fatal("both instances report leadership — advisory lock exclusivity violated")
	}
	if !leaderA.IsLeader() && !leaderB.IsLeader() {
		t.Fatal("no instance holds leadership after settling")
	}
}

// TestLeadership_KilledLeaderFailover is required test #2: killing the
// leader's underlying Postgres session (simulating a crashed/partitioned
// process — not merely calling Close(), which would just return the pooled
// connection for reuse without ending its session or releasing the
// session-scoped advisory lock) must let the other instance take over
// within the bounded heartbeat interval, and the job loop must resume
// (modeled here as the new leader's IsLeader() becoming true).
func TestLeadership_KilledLeaderFailover(t *testing.T) {
	db := openLeadershipTestDB(t)
	lockKey := uniqueLockKey(t)
	heartbeat := 200 * time.Millisecond

	leaderA := NewLeadership(db, LeadershipConfig{LockKey: lockKey, HeartbeatInterval: heartbeat, InstanceID: "leader-a"}, testLogger(t, "a"))
	leaderB := NewLeadership(db, LeadershipConfig{LockKey: lockKey, HeartbeatInterval: heartbeat, InstanceID: "leader-b"}, testLogger(t, "b"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go leaderA.Run(ctx)
	go leaderB.Run(ctx)

	if !awaitLeader(t, 5*time.Second, func() bool { return leaderA.IsLeader() != leaderB.IsLeader() }) {
		t.Fatal("election did not settle to exactly one leader within timeout")
	}

	leader, follower := leaderA, leaderB
	if leaderB.IsLeader() {
		leader, follower = leaderB, leaderA
	}
	if !leader.IsLeader() {
		t.Fatal("expected exactly one of the two to be leader by now")
	}

	// Find the leader's backend PID (white-box: same package, unexported
	// field access) and terminate that backend from a fresh admin session —
	// this is a true process/session kill, unlike Conn.Close() which would
	// merely return a healthy connection to sql.DB's pool for reuse.
	leader.mu.RLock()
	pinnedConn := leader.conn
	leader.mu.RUnlock()
	if pinnedConn == nil {
		t.Fatal("leader has no pinned connection")
	}
	var pid int
	if err := pinnedConn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatalf("get leader backend pid: %v", err)
	}

	adminConn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open admin conn: %v", err)
	}
	defer func() { _ = adminConn.Close() }()
	if _, err := adminConn.ExecContext(ctx, "SELECT pg_terminate_backend($1)", pid); err != nil {
		t.Fatalf("terminate leader backend: %v", err)
	}

	// The follower must assume leadership within a small bounded number of
	// heartbeat intervals (failover latency is documented as bounded by
	// HeartbeatInterval).
	if !awaitLeader(t, 5*time.Second, follower.IsLeader) {
		t.Fatal("follower did not assume leadership after the leader's backend was killed")
	}

	// The killed instance must notice too (its own reverify query now fails
	// against a dead connection) rather than continuing to believe it's
	// leader forever — this is the split-brain guard in the real-DB path.
	if !awaitLeader(t, 5*time.Second, func() bool { return !leader.IsLeader() }) {
		t.Fatal("killed leader never demoted itself")
	}
}

// lockKeySeq hands out distinct advisory-lock keys to each test in this
// process so tests never collide on the same lock even though they share
// one Postgres database.
var lockKeySeq int64 = time.Now().UnixNano()

// uniqueLockKey returns a lock key not used by any other test in this run.
func uniqueLockKey(t *testing.T) int64 {
	t.Helper()
	lockKeySeq++
	return lockKeySeq
}
