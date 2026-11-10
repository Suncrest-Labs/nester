package db_test

import (
	"context"
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
