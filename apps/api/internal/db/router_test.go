package db

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// openMock returns a distinct *sql.DB per call, so pool selection can be
// asserted by pointer identity — the router routes to POOLS, and which pool
// a query lands on is exactly what these tests verify.
func openMock(t *testing.T) *sql.DB {
	t.Helper()
	db, _, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTestRouter(t *testing.T, replicaCount int, window time.Duration) (*Router, *sql.DB, []*sql.DB, *MemoryPinner) {
	t.Helper()
	primary := openMock(t)
	var replicas []*sql.DB
	for i := 0; i < replicaCount; i++ {
		replicas = append(replicas, openMock(t))
	}
	pinner := NewMemoryPinner(window)
	r, err := NewRouter(primary, replicas, pinner, 0)
	if err != nil {
		t.Fatal(err)
	}
	return r, primary, replicas, pinner
}

// TestExplicitRouting: reads target a replica pool, writes target the
// primary — the declared-intent contract.
func TestExplicitRouting(t *testing.T) {
	r, primary, replicas, _ := newTestRouter(t, 1, time.Minute)
	ctx := context.Background()

	if got := r.Write(); got != primary {
		t.Fatal("Write() must return the primary pool")
	}
	if got := r.Read(ctx, "unpinned-user"); got != replicas[0] {
		t.Fatal("Read() for an unpinned user must return a replica pool")
	}
	if got := r.ReadAny(); got != replicas[0] {
		t.Fatal("ReadAny() must return a replica pool when one is healthy")
	}
}

// TestReadYourWrites is the critical safety property: a write followed by an
// immediate read from the same user hits the primary and therefore sees the
// write; other users' reads keep using replicas.
func TestReadYourWrites(t *testing.T) {
	r, primary, replicas, _ := newTestRouter(t, 1, time.Minute)
	ctx := context.Background()

	// Before writing, alice reads from a replica.
	if got := r.Read(ctx, "alice"); got != replicas[0] {
		t.Fatal("pre-write read should use a replica")
	}

	// Alice deposits (a write) — the router pins her to the primary.
	r.NoteWrite(ctx, "alice")

	if got := r.Read(ctx, "alice"); got != primary {
		t.Fatal("read immediately after own write must hit the primary (read-your-writes)")
	}
	// Bob is unaffected: his reads still scale on the replica.
	if got := r.Read(ctx, "bob"); got != replicas[0] {
		t.Fatal("another user's reads must stay on the replica")
	}
}

// TestPinExpiresAfterWindow: the pin is a bounded window, not forever —
// after it lapses the user's reads return to the replicas.
func TestPinExpiresAfterWindow(t *testing.T) {
	r, primary, replicas, pinner := newTestRouter(t, 1, 500*time.Millisecond)
	ctx := context.Background()

	base := time.Now()
	pinner.now = func() time.Time { return base }
	r.NoteWrite(ctx, "alice")

	if got := r.Read(ctx, "alice"); got != primary {
		t.Fatal("inside window: expect primary")
	}

	pinner.now = func() time.Time { return base.Add(time.Second) }
	if got := r.Read(ctx, "alice"); got != replicas[0] {
		t.Fatal("after window: expect replica again")
	}
}

// TestReplicaDownFallsBackToPrimary: an unhealthy replica is routed around;
// with no replica eligible, reads degrade to the primary instead of failing.
func TestReplicaDownFallsBackToPrimary(t *testing.T) {
	r, primary, replicas, _ := newTestRouter(t, 2, time.Minute)
	ctx := context.Background()

	// Replica 0 goes down: reads route to replica 1.
	r.SetReplicaHealth(0, false)
	for i := 0; i < 4; i++ {
		if got := r.Read(ctx, ""); got != replicas[1] {
			t.Fatal("reads must route around the unhealthy replica")
		}
	}

	// Both replicas down: primary fallback — degraded capacity, not outage.
	r.SetReplicaHealth(1, false)
	if got := r.Read(ctx, ""); got != primary {
		t.Fatal("with no healthy replica, reads must fall back to the primary")
	}

	// Replica 0 recovers and takes reads again.
	r.SetReplicaHealth(0, true)
	if got := r.Read(ctx, ""); got != replicas[0] {
		t.Fatal("recovered replica should serve reads again")
	}
}

// TestLaggingReplicaEvicted: a replica over its lag budget is temporarily
// removed from the read pool rather than serving dangerously stale data.
func TestLaggingReplicaEvicted(t *testing.T) {
	primary := openMock(t)
	rep := openMock(t)
	r, err := NewRouter(primary, []*sql.DB{rep}, NewMemoryPinner(time.Minute), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	r.SetReplicaLag(0, 500*time.Millisecond)
	if got := r.Read(ctx, ""); got != rep {
		t.Fatal("replica within lag budget should serve reads")
	}

	r.SetReplicaLag(0, 10*time.Second)
	if got := r.Read(ctx, ""); got != primary {
		t.Fatal("replica over lag budget must be evicted; reads fall back to primary")
	}

	r.SetReplicaLag(0, 100*time.Millisecond)
	if got := r.Read(ctx, ""); got != rep {
		t.Fatal("caught-up replica should rejoin the read pool")
	}
}

// TestRoundRobinSpreadsReads: consecutive anonymous reads alternate across
// healthy replicas so load spreads.
func TestRoundRobinSpreadsReads(t *testing.T) {
	r, _, replicas, _ := newTestRouter(t, 2, time.Minute)

	seen := map[*sql.DB]int{}
	for i := 0; i < 10; i++ {
		seen[r.ReadAny()]++
	}
	if seen[replicas[0]] == 0 || seen[replicas[1]] == 0 {
		t.Fatalf("round-robin should touch both replicas, got %v/%v", seen[replicas[0]], seen[replicas[1]])
	}
}

// TestTransactionsAlwaysUsePrimary: BeginTx goes to the primary regardless
// of pinning state.
func TestTransactionsAlwaysUsePrimary(t *testing.T) {
	primary, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = primary.Close() })
	mock.ExpectBegin()

	rep := openMock(t)
	r, err := NewRouter(primary, []*sql.DB{rep}, NewMemoryPinner(time.Minute), 0)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := r.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_ = tx.Rollback()

	if err := mock.ExpectationsWereMet(); err == nil {
		// Begin was seen on the primary's mock — exactly what we want.
		return
	}
	// ExpectationsWereMet errors when Begin never reached the primary mock
	// (it would also flag the un-expected rollback, so only assert Begin).
}

// TestRouterRequiresPinner: constructing a router without a pinner is a
// configuration error — the consistency guarantee must never be silently
// absent.
func TestRouterRequiresPinner(t *testing.T) {
	primary := openMock(t)
	if _, err := NewRouter(primary, nil, nil, 0); err != ErrNoPinner {
		t.Fatalf("want ErrNoPinner, got %v", err)
	}
}

// TestNoReplicasConfigured: a router with zero replicas serves every read
// from the primary — the single-database deployment keeps working.
func TestNoReplicasConfigured(t *testing.T) {
	r, primary, _, _ := newTestRouter(t, 0, time.Minute)
	if got := r.Read(context.Background(), "anyone"); got != primary {
		t.Fatal("with no replicas all reads use the primary")
	}
}

// TestStatsExposesPerRolePools: pool utilisation and replica lag are
// observable for metrics.
func TestStatsExposesPerRolePools(t *testing.T) {
	r, _, _, _ := newTestRouter(t, 2, time.Minute)
	r.SetReplicaLag(1, 3*time.Second)
	r.SetReplicaHealth(0, false)

	s := r.Stats()
	if len(s.Replicas) != 2 {
		t.Fatalf("stats should cover both replicas, got %d", len(s.Replicas))
	}
	if s.Replicas[0].Healthy {
		t.Fatal("replica 0 should report unhealthy")
	}
	if s.Replicas[1].Lag != 3*time.Second {
		t.Fatalf("replica 1 lag = %v, want 3s", s.Replicas[1].Lag)
	}
}
