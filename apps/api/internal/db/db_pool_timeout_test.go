package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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

func TestApplySessionTimeouts(t *testing.T) {
	newConnConfig := func(t *testing.T) *pgx.ConnConfig {
		t.Helper()
		c, err := pgx.ParseConfig("postgres://user:pass@localhost:5432/db?sslmode=disable")
		if err != nil {
			t.Fatalf("pgx.ParseConfig: %v", err)
		}
		return c
	}

	t.Run("uses defaults when unset", func(t *testing.T) {
		c := newConnConfig(t)
		db.ApplySessionTimeouts(c, 0, 0, 0, false)

		assertRuntimeParam(t, c, "statement_timeout", "30000")
		assertRuntimeParam(t, c, "lock_timeout", "5000")
		assertRuntimeParam(t, c, "idle_in_transaction_session_timeout", "60000")
	})

	t.Run("uses stricter defaults on mainnet", func(t *testing.T) {
		c := newConnConfig(t)
		db.ApplySessionTimeouts(c, 0, 0, 0, true)

		assertRuntimeParam(t, c, "statement_timeout", "10000")
		assertRuntimeParam(t, c, "lock_timeout", "2000")
		assertRuntimeParam(t, c, "idle_in_transaction_session_timeout", "30000")
	})

	t.Run("explicit values win over mainnet defaults", func(t *testing.T) {
		c := newConnConfig(t)
		db.ApplySessionTimeouts(c, 2*time.Second, 3*time.Second, 4*time.Second, true)

		assertRuntimeParam(t, c, "statement_timeout", "2000")
		assertRuntimeParam(t, c, "lock_timeout", "3000")
		assertRuntimeParam(t, c, "idle_in_transaction_session_timeout", "4000")
	})

	t.Run("negative disables the timeout", func(t *testing.T) {
		c := newConnConfig(t)
		db.ApplySessionTimeouts(c, -1, -1, -1, false)

		for _, key := range []string{
			"statement_timeout",
			"lock_timeout",
			"idle_in_transaction_session_timeout",
		} {
			if got, ok := c.RuntimeParams[key]; ok {
				t.Fatalf("%s should be absent when disabled, got %q", key, got)
			}
		}
	})
}

func assertRuntimeParam(t *testing.T, c *pgx.ConnConfig, key, want string) {
	t.Helper()
	if got := c.RuntimeParams[key]; got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}
