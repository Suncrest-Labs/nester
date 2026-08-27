package rotation

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/crypto"
)

// memWrapStore is an in-memory WrapStore.
//
// It can be told to fail on a specific row so that partial-failure and
// resumability can be exercised deterministically, which is the property the
// incident runbook relies on when a rotation is interrupted.
type memWrapStore struct {
	rows map[uuid.UUID]*WrappedRow
	// order preserves a deterministic scan order.
	order []uuid.UUID
	// failOn causes UpdateWrappedKey to fail for this row.
	failOn uuid.UUID
	// updates counts successful writes.
	updates int
}

func newMemWrapStore() *memWrapStore {
	return &memWrapStore{rows: make(map[uuid.UUID]*WrappedRow)}
}

func (s *memWrapStore) add(row WrappedRow) {
	copied := row
	s.rows[row.ID] = &copied
	s.order = append(s.order, row.ID)
}

func (s *memWrapStore) CountPendingWraps(_ context.Context, activeVersion string) (int, error) {
	n := 0
	for _, r := range s.rows {
		if r.KeyVersion != activeVersion {
			n++
		}
	}
	return n, nil
}

func (s *memWrapStore) ScanPendingWraps(_ context.Context, activeVersion string, limit int) ([]WrappedRow, error) {
	out := make([]WrappedRow, 0, limit)
	for _, id := range s.order {
		r := s.rows[id]
		if r.KeyVersion == activeVersion {
			continue
		}
		out = append(out, *r)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *memWrapStore) UpdateWrappedKey(_ context.Context, id uuid.UUID, wrapped []byte, version string) error {
	if id == s.failOn {
		return errors.New("simulated write failure")
	}
	r, ok := s.rows[id]
	if !ok {
		return errors.New("row not found")
	}
	// A single atomic write of both fields. A store that updated the version
	// without the wrapped key would render the row permanently unreadable.
	r.WrappedDataKey = wrapped
	r.KeyVersion = version
	s.updates++
	return nil
}

// testKeyB64 generates a fresh random 32-byte key.
//
// Random rather than deterministic: a test key with a predictable pattern is
// one copy-paste away from becoming a production key.
func testKeyB64(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func quietOptions() Options {
	return Options{BatchSize: 2, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// seedRows seals n records under oldCipher and loads them into the store.
func seedRows(t *testing.T, store *memWrapStore, oldCipher *crypto.EnvelopeCipher, n int) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, 0, n)
	for i := 0; i < n; i++ {
		sealed, err := oldCipher.Seal("record-value", nil)
		if err != nil {
			t.Fatalf("seal: %v", err)
		}
		id := uuid.New()
		store.add(WrappedRow{
			ID:             id,
			WrappedDataKey: sealed.WrappedDataKey,
			KeyVersion:     sealed.KeyVersion,
		})
		ids = append(ids, id)
	}
	return ids
}

func newCipherPair(t *testing.T) (oldC, newC *crypto.EnvelopeCipher) {
	t.Helper()
	keys := map[string]string{"v1": testKeyB64(t), "v2": testKeyB64(t)}
	var err error
	oldC, err = crypto.NewEnvelopeCipher("v1", keys)
	if err != nil {
		t.Fatalf("build v1 cipher: %v", err)
	}
	newC, err = crypto.NewEnvelopeCipher("v2", keys)
	if err != nil {
		t.Fatalf("build v2 cipher: %v", err)
	}
	return oldC, newC
}

func TestRewrapRotatesAllPendingRows(t *testing.T) {
	oldC, newC := newCipherPair(t)
	store := newMemWrapStore()
	seedRows(t, store, oldC, 5)

	stats, err := NewRewrapRotator(store, newC).Run(context.Background(), quietOptions())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stats.Pending != 5 {
		t.Fatalf("expected 5 pending, got %d", stats.Pending)
	}
	if stats.Rewrapped != 5 {
		t.Fatalf("expected 5 rewrapped, got %d", stats.Rewrapped)
	}

	for _, r := range store.rows {
		if r.KeyVersion != "v2" {
			t.Fatalf("row %s is still on %q", r.ID, r.KeyVersion)
		}
	}
}

func TestRewrapPreservesDecryptability(t *testing.T) {
	// The point of rotation is that the data stays readable afterwards.
	keys := map[string]string{"v1": testKeyB64(t), "v2": testKeyB64(t)}
	oldC, err := crypto.NewEnvelopeCipher("v1", keys)
	if err != nil {
		t.Fatalf("build v1: %v", err)
	}
	newC, err := crypto.NewEnvelopeCipher("v2", keys)
	if err != nil {
		t.Fatalf("build v2: %v", err)
	}

	const plaintext = "must-survive-rotation"
	sealed, err := oldC.Seal(plaintext, nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	store := newMemWrapStore()
	id := uuid.New()
	store.add(WrappedRow{ID: id, WrappedDataKey: sealed.WrappedDataKey, KeyVersion: sealed.KeyVersion})

	if _, err := NewRewrapRotator(store, newC).Run(context.Background(), quietOptions()); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Reassemble the row with its untouched ciphertext and the new wrapped key.
	rotated := crypto.SealedRecord{
		KeyVersion:     store.rows[id].KeyVersion,
		WrappedDataKey: store.rows[id].WrappedDataKey,
		Ciphertext:     sealed.Ciphertext,
	}
	opened, err := newC.Open(rotated, nil)
	if err != nil {
		t.Fatalf("record was unreadable after rotation: %v", err)
	}
	if opened != plaintext {
		t.Fatalf("rotation corrupted the value: %q", opened)
	}
}

func TestRewrapIsIdempotent(t *testing.T) {
	oldC, newC := newCipherPair(t)
	store := newMemWrapStore()
	seedRows(t, store, oldC, 3)

	rotator := NewRewrapRotator(store, newC)
	if _, err := rotator.Run(context.Background(), quietOptions()); err != nil {
		t.Fatalf("first run: %v", err)
	}
	writesAfterFirst := store.updates

	// A second run must find nothing to do.
	stats, err := rotator.Run(context.Background(), quietOptions())
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if stats.Pending != 0 || stats.Rewrapped != 0 {
		t.Fatalf("second run did work: pending=%d rewrapped=%d", stats.Pending, stats.Rewrapped)
	}
	if store.updates != writesAfterFirst {
		t.Fatal("second run wrote to the store")
	}
}

func TestRewrapIsResumableAfterPartialFailure(t *testing.T) {
	oldC, newC := newCipherPair(t)
	store := newMemWrapStore()
	ids := seedRows(t, store, oldC, 5)

	// Fail on the third row. Rows before it must stay committed.
	store.failOn = ids[2]

	rotator := NewRewrapRotator(store, newC)
	stats, err := rotator.Run(context.Background(), quietOptions())
	if err == nil {
		t.Fatal("a failing run reported success")
	}
	if stats.Rewrapped == 0 {
		t.Fatal("no rows were committed before the failure; the run was not resumable")
	}

	// Some rows are on v2, the rest still on v1 — a genuine mixed-version
	// dataset, which reads must tolerate.
	pending, err := store.CountPendingWraps(context.Background(), "v2")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if pending == 0 {
		t.Fatal("expected rows to remain pending after the failure")
	}

	// Clear the fault and resume. The run must complete without redoing
	// committed work incorrectly.
	store.failOn = uuid.Nil
	stats2, err := rotator.Run(context.Background(), quietOptions())
	if err != nil {
		t.Fatalf("resumed run failed: %v", err)
	}
	if stats2.Rewrapped != pending {
		t.Fatalf("resumed run rewrapped %d rows, expected %d", stats2.Rewrapped, pending)
	}

	for _, r := range store.rows {
		if r.KeyVersion != "v2" {
			t.Fatalf("row %s still on %q after the resumed run", r.ID, r.KeyVersion)
		}
	}
}

func TestRewrapReportsIncompleteRun(t *testing.T) {
	// A run that cannot drain must not report success: reporting success is
	// the precondition for retiring a key that rows still need.
	oldC, newC := newCipherPair(t)
	store := &stubbornStore{memWrapStore: newMemWrapStore()}
	seedRows(t, store.memWrapStore, oldC, 2)

	_, err := NewRewrapRotator(store, newC).Run(context.Background(), quietOptions())
	if !errors.Is(err, ErrRewrapIncomplete) {
		t.Fatalf("expected ErrRewrapIncomplete, got %v", err)
	}
}

// stubbornStore reports rows as pending even after they are updated, standing
// in for a store where concurrent writes keep introducing new pending rows.
type stubbornStore struct {
	*memWrapStore
	scans int
}

func (s *stubbornStore) ScanPendingWraps(ctx context.Context, active string, limit int) ([]WrappedRow, error) {
	// Serve rows once, then report none — so the loop terminates — while
	// CountPendingWraps still reports work outstanding.
	s.scans++
	if s.scans > 1 {
		return nil, nil
	}
	return s.memWrapStore.ScanPendingWraps(ctx, active, limit)
}

func (s *stubbornStore) CountPendingWraps(context.Context, string) (int, error) {
	return 1, nil
}

func TestRewrapRespectsContextCancellation(t *testing.T) {
	oldC, newC := newCipherPair(t)
	store := newMemWrapStore()
	seedRows(t, store, oldC, 10)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewRewrapRotator(store, newC).Run(ctx, quietOptions())
	if err == nil {
		t.Fatal("a cancelled run reported success")
	}
}

func TestRewrapFailsWhenOldKeyRetired(t *testing.T) {
	// Rotation must fail loudly if a key it needs has been removed, rather
	// than skipping the affected rows.
	keys := map[string]string{"v1": testKeyB64(t), "v2": testKeyB64(t)}
	oldC, err := crypto.NewEnvelopeCipher("v1", keys)
	if err != nil {
		t.Fatalf("build v1: %v", err)
	}
	store := newMemWrapStore()
	seedRows(t, store, oldC, 2)

	// v1 dropped from the configuration while rows still reference it.
	withoutV1, err := crypto.NewEnvelopeCipher("v2", map[string]string{"v2": keys["v2"]})
	if err != nil {
		t.Fatalf("build v2-only: %v", err)
	}

	if _, err := NewRewrapRotator(store, withoutV1).Run(context.Background(), quietOptions()); err == nil {
		t.Fatal("rotation succeeded with a retired key still in use")
	}
}

func TestVerifyRetirable(t *testing.T) {
	oldC, newC := newCipherPair(t)
	store := newMemWrapStore()
	seedRows(t, store, oldC, 3)
	ctx := context.Background()

	t.Run("refuses while rows remain on the old version", func(t *testing.T) {
		if err := VerifyRetirable(ctx, store, "v1", "v2"); err == nil {
			t.Fatal("retiring v1 was permitted while rows still use it")
		}
	})

	t.Run("refuses to retire the active version", func(t *testing.T) {
		if err := VerifyRetirable(ctx, store, "v2", "v2"); err == nil {
			t.Fatal("retiring the active version was permitted")
		}
	})

	t.Run("permits once rotation is complete", func(t *testing.T) {
		if _, err := NewRewrapRotator(store, newC).Run(ctx, quietOptions()); err != nil {
			t.Fatalf("run: %v", err)
		}
		if err := VerifyRetirable(ctx, store, "v1", "v2"); err != nil {
			t.Fatalf("retiring v1 was refused after a complete rotation: %v", err)
		}
	})
}
