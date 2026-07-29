package valuation

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/portfolio"
)

type fakePositions struct {
	calls int
	list  []Position
}

func (f *fakePositions) Positions(context.Context, uuid.UUID) ([]Position, error) {
	f.calls++
	return f.list, nil
}

type recordingNotifier struct {
	mu     sync.Mutex
	pushed []portfolio.Valuation
}

func (n *recordingNotifier) PushValuation(_ uuid.UUID, v portfolio.Valuation) {
	n.mu.Lock()
	n.pushed = append(n.pushed, v)
	n.mu.Unlock()
}

func (n *recordingNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.pushed)
}

func newTestService(pos *fakePositions, notifier Notifier) *Service {
	return NewService(Deps{
		Positions: pos,
		Oracle:    NewStaticOracle(nil),
		Cache:     NewCache(time.Minute),
		Notifier:  notifier,
	})
}

func TestService_GetValuationCaches(t *testing.T) {
	pos := &fakePositions{list: []Position{{VaultID: uuid.New(), Asset: "USDC", Principal: dec("10")}}}
	svc := newTestService(pos, nil)
	uid := uuid.New()

	if _, err := svc.GetValuation(context.Background(), uid); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := svc.GetValuation(context.Background(), uid); err != nil {
		t.Fatalf("second: %v", err)
	}
	if pos.calls != 1 {
		t.Fatalf("position source hit %d times, want 1 (second served from cache)", pos.calls)
	}
}

func TestService_InvalidateRecomputesAndPushes(t *testing.T) {
	pos := &fakePositions{list: []Position{{VaultID: uuid.New(), Asset: "USDC", Principal: dec("10")}}}
	notifier := &recordingNotifier{}
	svc := newTestService(pos, notifier)
	uid := uuid.New()

	// Prime the cache.
	if _, err := svc.GetValuation(context.Background(), uid); err != nil {
		t.Fatalf("prime: %v", err)
	}

	svc.Invalidate(uid)

	// Invalidate recomputes+pushes asynchronously.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && notifier.count() == 0 {
		time.Sleep(2 * time.Millisecond)
	}
	if notifier.count() != 1 {
		t.Fatalf("expected 1 push after invalidation, got %d", notifier.count())
	}

	// The cache was refreshed by the recompute (calls: prime=1, recompute=1).
	if pos.calls != 2 {
		t.Fatalf("position source hit %d times, want 2", pos.calls)
	}
	if _, ok := svc.cache.Get(uid); !ok {
		t.Fatal("expected cache to be repopulated after invalidation recompute")
	}
}
