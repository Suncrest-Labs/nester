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
	const (
		baseDSN         = "postgres://user:pass@localhost:5432/db?sslmode=disable"
		dsnWithTimeouts = baseDSN + "&statement_timeout=12345&lock_timeout=2345&idle_in_transaction_session_timeout=3456"
		statementParam  = "statement_timeout"
		lockParam       = "lock_timeout"
		idleParam       = "idle_in_transaction_session_timeout"
	)

	type wantParams struct {
		statement string
		lock      string
		idle      string
	}

	cases := []struct {
		name      string
		dsn       string
		statement time.Duration
		lock      time.Duration
		idle      time.Duration
		strict    bool
		want      wantParams
	}{
		{
			name: "uses defaults when unset",
			dsn:  baseDSN,
			want: wantParams{statement: "30000", lock: "5000", idle: "60000"},
		},
		{
			name:   "uses stricter defaults on mainnet",
			dsn:    baseDSN,
			strict: true,
			want:   wantParams{statement: "10000", lock: "2000", idle: "30000"},
		},
		{
			name:      "explicit values win over mainnet defaults",
			dsn:       baseDSN,
			statement: 2 * time.Second,
			lock:      3 * time.Second,
			idle:      4 * time.Second,
			strict:    true,
			want:      wantParams{statement: "2000", lock: "3000", idle: "4000"},
		},
		{
			name:      "mix of default, override and disabled",
			dsn:       baseDSN,
			statement: 2 * time.Second,
			lock:      -1,
			want:      wantParams{statement: "2000", lock: "0", idle: "60000"},
		},
		{
			name:      "negative disables and overrides DSN values",
			dsn:       dsnWithTimeouts,
			statement: -1,
			lock:      -1,
			idle:      -1,
			want:      wantParams{statement: "0", lock: "0", idle: "0"},
		},
		{
			name:      "positive sub-millisecond rounds up to 1ms",
			dsn:       baseDSN,
			statement: 500 * time.Microsecond,
			lock:      500 * time.Microsecond,
			idle:      500 * time.Microsecond,
			want:      wantParams{statement: "1", lock: "1", idle: "1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := pgx.ParseConfig(tc.dsn)
			if err != nil {
				t.Fatalf("pgx.ParseConfig: %v", err)
			}

			db.ApplySessionTimeouts(c, tc.statement, tc.lock, tc.idle, tc.strict)

			assertRuntimeParam(t, c, statementParam, tc.want.statement)
			assertRuntimeParam(t, c, lockParam, tc.want.lock)
			assertRuntimeParam(t, c, idleParam, tc.want.idle)
		})
	}
}

func assertRuntimeParam(t *testing.T, c *pgx.ConnConfig, key, want string) {
	t.Helper()
	if got := c.RuntimeParams[key]; got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}
