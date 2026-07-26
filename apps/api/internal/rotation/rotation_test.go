package rotation_test

import (
	"context"
	"encoding/base64"
	"errors"
	"sort"
	"testing"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/crypto"
	"github.com/suncrestlabs/nester/apps/api/internal/rotation"
)

func key(b byte) string {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = b
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// memStore is an in-memory rotation.Store. failAfter, when > 0, makes
// UpdateCipher return an error once that many updates have succeeded, simulating
// an interrupted run.
type memStore struct {
	rows      map[uuid.UUID]rotation.EncryptedRow
	updates   int
	failAfter int
}

func newMemStore() *memStore {
	return &memStore{rows: make(map[uuid.UUID]rotation.EncryptedRow)}
}

func (m *memStore) put(row rotation.EncryptedRow) {
	m.rows[row.ID] = row
}

func (m *memStore) CountPending(_ context.Context, active string) (int, error) {
	n := 0
	for _, r := range m.rows {
		if r.KeyVersion != active {
			n++
		}
	}
	return n, nil
}

func (m *memStore) ScanPending(_ context.Context, active string, limit int) ([]rotation.EncryptedRow, error) {
	var pending []rotation.EncryptedRow
	for _, r := range m.rows {
		if r.KeyVersion != active {
			pending = append(pending, r)
		}
	}
	// Deterministic order for test stability.
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].ID.String() < pending[j].ID.String()
	})
	if len(pending) > limit {
		pending = pending[:limit]
	}
	return pending, nil
}

var errInjected = errors.New("injected update failure")

func (m *memStore) UpdateCipher(_ context.Context, id uuid.UUID, ciphertext []byte, keyVersion string) error {
	if m.failAfter > 0 && m.updates >= m.failAfter {
		return errInjected
	}
	m.rows[id] = rotation.EncryptedRow{ID: id, Ciphertext: ciphertext, KeyVersion: keyVersion}
	m.updates++
	return nil
}

// seedLegacyRows encrypts n account numbers with the v1 single-key cipher and
// stores them, returning the plaintext keyed by row ID for later verification.
func seedLegacyRows(t *testing.T, store *memStore, n int) map[uuid.UUID]string {
	t.Helper()
	v1, err := crypto.NewAccountCipher(key(1))
	if err != nil {
		t.Fatalf("v1 cipher: %v", err)
	}
	want := make(map[uuid.UUID]string, n)
	for i := 0; i < n; i++ {
		id := uuid.New()
		plain := "acct-" + id.String()[:8]
		env, err := v1.Encrypt(plain)
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		store.put(rotation.EncryptedRow{ID: id, Ciphertext: env.Ciphertext, KeyVersion: env.KeyVersion})
		want[id] = plain
	}
	return want
}

func multiCipher(t *testing.T) *crypto.AccountCipher {
	t.Helper()
	c, err := crypto.NewAccountCipherWithKeys("v2", map[string]string{"v1": key(1), "v2": key(2)}, "")
	if err != nil {
		t.Fatalf("multi cipher: %v", err)
	}
	return c
}

func TestRotator_ReencryptsToActive(t *testing.T) {
	store := newMemStore()
	want := seedLegacyRows(t, store, 5)
	cipher := multiCipher(t)

	stats, err := rotator(store, cipher).Run(context.Background(), rotation.Options{BatchSize: 2})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stats.Pending != 5 || stats.Rotated != 5 {
		t.Fatalf("stats = %+v, want pending=5 rotated=5", stats)
	}

	for id, plain := range want {
		row := store.rows[id]
		if row.KeyVersion != "v2" {
			t.Fatalf("row %s version = %q, want v2", id, row.KeyVersion)
		}
		got, err := cipher.Decrypt(crypto.CipherEnvelope{KeyVersion: row.KeyVersion, Ciphertext: row.Ciphertext})
		if err != nil {
			t.Fatalf("decrypt rotated row: %v", err)
		}
		if got != plain {
			t.Fatalf("row %s plaintext = %q, want %q", id, got, plain)
		}
	}
}

func TestRotator_Idempotent(t *testing.T) {
	store := newMemStore()
	seedLegacyRows(t, store, 4)
	cipher := multiCipher(t)

	if _, err := rotator(store, cipher).Run(context.Background(), rotation.Options{BatchSize: 3}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := rotator(store, cipher).Run(context.Background(), rotation.Options{BatchSize: 3})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if second.Pending != 0 || second.Rotated != 0 {
		t.Fatalf("second run stats = %+v, want zero work", second)
	}
}

func TestRotator_ResumableAfterInterrupt(t *testing.T) {
	store := newMemStore()
	want := seedLegacyRows(t, store, 6)
	store.failAfter = 4 // fail partway through
	cipher := multiCipher(t)

	// First run is interrupted by the injected failure.
	if _, err := rotator(store, cipher).Run(context.Background(), rotation.Options{BatchSize: 10}); !errors.Is(err, errInjected) {
		t.Fatalf("want injected error, got %v", err)
	}

	// Some rows rotated, some remain — the run made partial, durable progress.
	remaining, _ := store.CountPending(context.Background(), "v2")
	if remaining == 0 || remaining == 6 {
		t.Fatalf("expected partial progress, got %d pending of 6", remaining)
	}

	// Clear the fault and resume; the run must complete without redoing work
	// destructively or losing data.
	store.failAfter = 0
	if _, err := rotator(store, cipher).Run(context.Background(), rotation.Options{BatchSize: 10}); err != nil {
		t.Fatalf("resume run: %v", err)
	}

	done, _ := store.CountPending(context.Background(), "v2")
	if done != 0 {
		t.Fatalf("expected all rows rotated after resume, %d still pending", done)
	}
	for id, plain := range want {
		row := store.rows[id]
		got, err := cipher.Decrypt(crypto.CipherEnvelope{KeyVersion: row.KeyVersion, Ciphertext: row.Ciphertext})
		if err != nil {
			t.Fatalf("decrypt row %s: %v", id, err)
		}
		if got != plain {
			t.Fatalf("row %s data corrupted: got %q want %q", id, got, plain)
		}
	}
}

func TestRotator_LegacyDataReadableBeforeAndAfter(t *testing.T) {
	store := newMemStore()
	want := seedLegacyRows(t, store, 3)
	cipher := multiCipher(t)

	// Before rotation: rows are on v1 and must already decrypt via the multi-key
	// cipher (v1 retained for decryption).
	for id, plain := range want {
		row := store.rows[id]
		got, err := cipher.Decrypt(crypto.CipherEnvelope{KeyVersion: row.KeyVersion, Ciphertext: row.Ciphertext})
		if err != nil || got != plain {
			t.Fatalf("pre-rotation decrypt of %s: got %q err %v", id, got, err)
		}
	}

	if _, err := rotator(store, cipher).Run(context.Background(), rotation.Options{}); err != nil {
		t.Fatalf("run: %v", err)
	}

	// After rotation: same plaintext, now on the active version.
	for id, plain := range want {
		row := store.rows[id]
		if row.KeyVersion != "v2" {
			t.Fatalf("row %s not rotated", id)
		}
		got, err := cipher.Decrypt(crypto.CipherEnvelope{KeyVersion: row.KeyVersion, Ciphertext: row.Ciphertext})
		if err != nil || got != plain {
			t.Fatalf("post-rotation decrypt of %s: got %q err %v", id, got, err)
		}
	}
}

func rotator(store rotation.Store, cipher rotation.Cipher) *rotation.Rotator {
	return rotation.NewRotator(store, cipher)
}
