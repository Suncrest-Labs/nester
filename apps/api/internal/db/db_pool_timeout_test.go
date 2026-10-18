package db_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/db"
)

func TestWithQueryTimeout(t *testing.T) {
	t.Run("applies default timeout when context has no deadline", func(t *testing.T) {
		ctx := context.Background()
		timeoutCtx, cancel := db.WithQueryTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		deadline, ok := timeoutCtx.Deadline()
		if !ok {
			t.Fatal("expected deadline to be set on context")
		}
		if time.Until(deadline) > 100*time.Millisecond {
			t.Fatalf("deadline is too far in future: %v", time.Until(deadline))
		}
	})

	t.Run("preserves existing deadline when already set", func(t *testing.T) {
		existingDeadline := time.Now().Add(50 * time.Millisecond)
		ctx, cancel := context.WithDeadline(context.Background(), existingDeadline)
		defer cancel()

		timeoutCtx, cancelTimeout := db.WithQueryTimeout(ctx, 5*time.Second)
		defer cancelTimeout()

		deadline, ok := timeoutCtx.Deadline()
		if !ok {
			t.Fatal("expected deadline on context")
		}
		if !deadline.Equal(existingDeadline) {
			t.Fatalf("expected existing deadline %v, got %v", existingDeadline, deadline)
		}
	})
}

func TestResolvedTimeouts(t *testing.T) {
	t.Run("uses standard defaults when Mainnet is false", func(t *testing.T) {
		stmt, lock, idleTx := (db.Config{}).ResolvedTimeouts()
		if stmt != db.DefaultStatementTimeout {
			t.Errorf("StatementTimeout = %v, want %v", stmt, db.DefaultStatementTimeout)
		}
		if lock != db.DefaultLockTimeout {
			t.Errorf("LockTimeout = %v, want %v", lock, db.DefaultLockTimeout)
		}
		if idleTx != db.DefaultIdleInTransactionTimeout {
			t.Errorf("IdleInTransactionSessionTimeout = %v, want %v", idleTx, db.DefaultIdleInTransactionTimeout)
		}
	})

	t.Run("uses stricter defaults when Mainnet is true", func(t *testing.T) {
		stmt, lock, idleTx := (db.Config{Mainnet: true}).ResolvedTimeouts()
		if stmt != db.MainnetStatementTimeout {
			t.Errorf("StatementTimeout = %v, want %v", stmt, db.MainnetStatementTimeout)
		}
		if lock != db.MainnetLockTimeout {
			t.Errorf("LockTimeout = %v, want %v", lock, db.MainnetLockTimeout)
		}
		if idleTx != db.MainnetIdleInTransactionTimeout {
			t.Errorf("IdleInTransactionSessionTimeout = %v, want %v", idleTx, db.MainnetIdleInTransactionTimeout)
		}
	})

	t.Run("preserves explicit overrides", func(t *testing.T) {
		cfg := db.Config{
			Mainnet:                         true,
			StatementTimeout:                1500 * time.Millisecond,
			LockTimeout:                     750 * time.Millisecond,
			IdleInTransactionSessionTimeout: 4 * time.Second,
		}
		stmt, lock, idleTx := cfg.ResolvedTimeouts()
		if stmt != 1500*time.Millisecond || lock != 750*time.Millisecond || idleTx != 4*time.Second {
			t.Fatalf("unexpected resolved timeouts: stmt=%v lock=%v idleTx=%v", stmt, lock, idleTx)
		}
	})
}

// TestStatementTimeoutCancelsPgSleep verifies that a server-side statement_timeout
// configured on db.Open cancels a slow pg_sleep query at the timeout (#1564),
// and that OverrideTxTimeouts allows migrations/backfill to exceed the base timeout.
func TestStatementTimeoutCancelsPgSleep(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set — skipping Postgres statement_timeout integration test")
	}

	pool, err := db.Open(db.Config{
		DSN:                             dsn,
		ConnectionTimeout:               5 * time.Second,
		StatementTimeout:                150 * time.Millisecond,
		LockTimeout:                     100 * time.Millisecond,
		IdleInTransactionSessionTimeout: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })

	start := time.Now()
	_, err = pool.ExecContext(context.Background(), "SELECT pg_sleep(2)")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected pg_sleep(2) to be cancelled by statement_timeout, got nil error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "statement timeout") && !strings.Contains(err.Error(), "57014") {
		t.Fatalf("expected statement timeout error (SQLSTATE 57014), got: %v", err)
	}
	if elapsed >= 1500*time.Millisecond {
		t.Fatalf("expected query to be cancelled well before 1.5s, took %v", elapsed)
	}

	// Verify OverrideBackfillTimeouts relaxes the timeout inside a transaction.
	tx, err := pool.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := db.OverrideBackfillTimeouts(context.Background(), tx); err != nil {
		t.Fatalf("OverrideBackfillTimeouts failed: %v", err)
	}
	if _, err := tx.ExecContext(context.Background(), "SELECT pg_sleep(0.25)"); err != nil {
		t.Fatalf("expected overridden transaction timeout to allow 250ms sleep, got: %v", err)
	}
}
