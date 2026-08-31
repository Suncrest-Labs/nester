package metrics

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// stubStater returns a fixed pgxpool.Stat, so the collector's mapping from
// stat fields to metric names can be tested without a database.
type stubStater struct {
	stat *pgxpool.Stat
}

func (s stubStater) Stat() *pgxpool.Stat { return s.stat }

func TestPoolCollectorDescribesAllMetrics(t *testing.T) {
	collector := NewPoolCollector(stubStater{})

	descs := make(chan *prometheus.Desc, 32)
	collector.Describe(descs)
	close(descs)

	var count int
	for desc := range descs {
		count++
		if !strings.Contains(desc.String(), "nester_db_pool_") {
			t.Errorf("descriptor is not namespaced: %s", desc)
		}
	}

	if count != 12 {
		t.Fatalf("expected 12 pool descriptors, got %d", count)
	}
}

func TestPoolCollectorNilPoolIsSafe(t *testing.T) {
	collector := NewPoolCollector(nil)

	metricsCh := make(chan prometheus.Metric, 8)
	collector.Collect(metricsCh)
	close(metricsCh)

	if len(metricsCh) != 0 {
		t.Fatalf("expected no metrics from a nil pool, got %d", len(metricsCh))
	}
}

func TestPoolCollectorNilStatIsSafe(t *testing.T) {
	collector := NewPoolCollector(stubStater{stat: nil})

	metricsCh := make(chan prometheus.Metric, 8)
	collector.Collect(metricsCh)
	close(metricsCh)

	if len(metricsCh) != 0 {
		t.Fatalf("expected no metrics from a nil stat, got %d", len(metricsCh))
	}
}

func TestRegisterPoolNilIsSafe(t *testing.T) {
	m := New()
	if err := m.RegisterPool(nil); err != nil {
		t.Fatalf("registering a nil pool should be a no-op, got %v", err)
	}
}

// TestPoolMetricsExposedIntegration verifies against a real pool that the
// pool metrics appear in a scrape, and — the acceptance criterion from the
// issue — that the acquire-wait metrics rise when the pool is exhausted.
//
// A pool of exactly one connection is used so exhaustion is deterministic:
// the first acquire takes the only connection, and the second must wait for
// it to be returned, which is precisely what EmptyAcquireCount counts.
//
// Skips without DATABASE_URL, following the convention in
// internal/middleware/ratelimit_backend_test.go. CI supplies it.
func TestPoolMetricsExposedIntegration(t *testing.T) {
	// TEST_DATABASE_DSN is the convention in internal/db/db_integration_test.go;
	// DATABASE_URL is what the CI job exports. Accept either so this runs in
	// CI without a workflow change and locally without a second variable.
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN/DATABASE_URL not set; skipping pool metrics integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	poolConfig.MaxConns = 1

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("database unreachable: %v", err)
	}

	m := New()
	if err := m.RegisterPool(pool); err != nil {
		t.Fatalf("register pool: %v", err)
	}

	before := counterValue(t, m.Registry(), "nester_db_pool_empty_acquire_waits_total", nil)

	// Hold the only connection, then force a second acquire to queue behind
	// it. The release happens on a timer so the waiter is guaranteed to have
	// blocked first rather than racing to an idle connection.
	held, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire first connection: %v", err)
	}

	released := make(chan struct{})
	go func() {
		time.Sleep(150 * time.Millisecond)
		held.Release()
		close(released)
	}()

	waiter, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire second connection: %v", err)
	}
	waiter.Release()
	<-released

	after := counterValue(t, m.Registry(), "nester_db_pool_empty_acquire_waits_total", nil)
	if after <= before {
		t.Fatalf("expected the empty-acquire counter to rise under exhaustion: before=%v after=%v", before, after)
	}

	waitSeconds := counterValue(t, m.Registry(), "nester_db_pool_acquire_wait_seconds_total", nil)
	if waitSeconds <= 0 {
		t.Fatalf("expected non-zero cumulative acquire wait, got %v", waitSeconds)
	}

	// The gauges must be present in the same scrape, so an operator reading
	// a saturation alert can see the pool shape that produced it.
	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	required := map[string]bool{
		"nester_db_pool_total_connections":    false,
		"nester_db_pool_idle_connections":     false,
		"nester_db_pool_acquired_connections": false,
		"nester_db_pool_max_connections":      false,
		"nester_db_pool_acquires_total":       false,
	}
	for _, family := range families {
		if _, ok := required[family.GetName()]; ok {
			required[family.GetName()] = true
		}
	}
	for name, present := range required {
		if !present {
			t.Errorf("expected %s in the exposition", name)
		}
	}

	// The DSN carries credentials; it must not appear anywhere in the
	// metrics output.
	for _, value := range allLabelValues(t, m.Registry()) {
		if strings.Contains(value, "postgres://") || strings.Contains(value, "password") {
			t.Fatalf("connection detail leaked into label %q", value)
		}
	}
}
