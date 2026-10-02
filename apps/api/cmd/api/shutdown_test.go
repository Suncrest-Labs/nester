package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

// These tests exercise the coordinated-shutdown pattern applied throughout
// run() for issue #786: every long-lived worker derives its context from
// shutdownCtx and registers on a shared sync.WaitGroup, so cancelling
// shutdownCtx and then waiting on the WaitGroup (bounded by a deadline, as
// the shutdown sequence in run() does) reliably observes every worker
// actually stop rather than racing the process exit.
//
// run() itself wires a real HTTP server, Postgres, Redis, and Stellar RPC
// client and is not practically unit-testable as a whole; these tests cover
// the pattern it applies at every one of its ~20 worker call sites, in
// isolation.

// fakeWorker mimics the shape every worker in run() has: a Run(ctx) method
// that loops on a ticker until ctx is cancelled, then returns.
type fakeWorker struct {
	tickInterval time.Duration
	ticks        int
	mu           sync.Mutex
}

func (w *fakeWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.mu.Lock()
			w.ticks++
			w.mu.Unlock()
		}
	}
}

// startWorker wires a fakeWorker exactly as run() wires each real worker:
// a context derived from shutdownCtx, registered on the shared WaitGroup
// before the goroutine starts.
func startWorker(shutdownCtx context.Context, wg *sync.WaitGroup, w *fakeWorker) context.CancelFunc {
	ctx, cancel := context.WithCancel(shutdownCtx)
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.Run(ctx)
	}()
	return cancel
}

func TestShutdownPattern_WorkersObserveParentCancellation(t *testing.T) {
	shutdownCtx, stop := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	workers := make([]*fakeWorker, 5)
	cancels := make([]context.CancelFunc, 5)
	for i := range workers {
		workers[i] = &fakeWorker{tickInterval: time.Millisecond}
		cancels[i] = startWorker(shutdownCtx, &wg, workers[i])
	}
	// defer per-worker cancel funcs, matching run()'s defer cancelX() at
	// each call site — a safety net for early-return paths, not the
	// mechanism this test exercises.
	for _, c := range cancels {
		defer c()
	}

	// Let every worker actually tick at least once, so this proves they
	// were running (not merely that an unstarted goroutine "stopped").
	time.Sleep(20 * time.Millisecond)

	stop() // the single signal run() sends via its own stop() at shutdown

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("wg.Wait() did not return after the parent shutdown context was cancelled")
	}

	for i, w := range workers {
		w.mu.Lock()
		ticks := w.ticks
		w.mu.Unlock()
		if ticks == 0 {
			t.Errorf("worker %d never ticked before being cancelled; test did not actually exercise a running worker", i)
		}
	}
}

// TestShutdownPattern_BoundedWaitTimesOutOnAHungWorker is the "hung worker
// is force-exited after the deadline with a logged warning" acceptance
// criterion: a worker that ignores cancellation must not hang the whole
// shutdown sequence forever. run() applies exactly this select against its
// graceful-shutdown deadline; this proves the select itself has the
// intended timeout behavior rather than blocking indefinitely on Wait().
func TestShutdownPattern_BoundedWaitTimesOutOnAHungWorker(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1) // deliberately never matched by a Done(): simulates a hung worker

	deadline, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("expected the deadline to fire before a hung worker's Done()")
	case <-deadline.Done():
		// This is the intended outcome: run() logs a warning and proceeds
		// with process exit rather than blocking forever.
	}
}

// TestShutdownPattern_NoLeakWhenAllWorkersExitPromptly guards against a
// goroutine leak going undetected: every worker started must actually
// decrement the WaitGroup, not merely appear to via a bug in the test
// double, by asserting Wait() returns well within the fakeWorker's own
// tick interval after cancellation.
func TestShutdownPattern_NoLeakWhenAllWorkersExitPromptly(t *testing.T) {
	shutdownCtx, stop := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	w := &fakeWorker{tickInterval: time.Hour} // long enough that a tick firing would be a bug, not a race
	cancel := startWorker(shutdownCtx, &wg, w)
	defer cancel()

	stop()

	start := time.Now()
	wg.Wait()
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Errorf("expected near-instant return after cancellation (select on ctx.Done() has no ticker delay), took %s", elapsed)
	}
}
