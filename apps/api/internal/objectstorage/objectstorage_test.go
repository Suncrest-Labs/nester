package objectstorage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *LocalDiskStore {
	t.Helper()
	dir := t.TempDir()
	store, err := NewLocalDiskStore(dir, 1024, []string{"image/jpeg", "image/png"})
	if err != nil {
		t.Fatalf("NewLocalDiskStore() error = %v", err)
	}
	return store
}

func TestLocalDiskStore_PutWritesRealBytesToDisk(t *testing.T) {
	store := newTestStore(t)
	content := []byte("this is a real uploaded file, not a mock")

	key, err := store.Put(context.Background(), "user-123", "image/jpeg", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	fullPath := filepath.Join(store.BaseDir, key)
	got, err := os.ReadFile(fullPath) // #nosec G304 -- test reads back exactly what Put wrote
	if err != nil {
		t.Fatalf("expected the file to exist on disk at %s: %v", fullPath, err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("stored content = %q, want %q", got, content)
	}
}

func TestLocalDiskStore_KeyIsServerGeneratedNotClientInfluenced(t *testing.T) {
	store := newTestStore(t)

	hostileFilenames := []string{
		"../../../etc/passwd",
		"..\\..\\windows\\system32\\config",
		"/etc/shadow",
		"a" + strings.Repeat("/../", 20) + "b",
	}

	for _, hostile := range hostileFilenames {
		// The Store interface never even accepts a filename — Put's
		// signature has no parameter a caller could use to influence the
		// key. This test documents that guarantee explicitly: passing a
		// hostile string as the keyPrefix (the only caller-influenced
		// input Put accepts, normally a trusted user id) is neutralized
		// rather than used verbatim.
		key, err := store.Put(context.Background(), hostile, "image/jpeg", bytes.NewReader([]byte("x")))
		if err != nil {
			t.Fatalf("Put() error = %v for keyPrefix %q", err, hostile)
		}
		if strings.Contains(key, "..") {
			t.Fatalf("generated key %q retains a path-traversal sequence from keyPrefix %q", key, hostile)
		}
		if filepath.IsAbs(key) {
			t.Fatalf("generated key %q is absolute (escapes BaseDir) for keyPrefix %q", key, hostile)
		}
		resolved := filepath.Join(store.BaseDir, key)
		if !strings.HasPrefix(resolved, filepath.Clean(store.BaseDir)+string(filepath.Separator)) {
			t.Fatalf("resolved path %q escapes BaseDir %q for keyPrefix %q", resolved, store.BaseDir, hostile)
		}
	}
}

func TestLocalDiskStore_RejectsDisallowedContentType(t *testing.T) {
	store := newTestStore(t)

	_, err := store.Put(context.Background(), "user-1", "application/x-executable", bytes.NewReader([]byte("x")))
	if !errors.Is(err, ErrContentTypeNotAllowed) {
		t.Fatalf("Put() error = %v, want ErrContentTypeNotAllowed", err)
	}
}

func TestLocalDiskStore_RejectsUploadsOverMaxBytes(t *testing.T) {
	store := newTestStore(t)
	oversized := bytes.Repeat([]byte("a"), int(store.MaxBytes)+1)

	_, err := store.Put(context.Background(), "user-1", "image/jpeg", bytes.NewReader(oversized))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Put() error = %v, want ErrTooLarge", err)
	}
}

func TestLocalDiskStore_RejectsUploadsAtExactlyMaxBytesPlusOne(t *testing.T) {
	store := newTestStore(t)
	exact := bytes.Repeat([]byte("a"), int(store.MaxBytes))

	if _, err := store.Put(context.Background(), "user-1", "image/jpeg", bytes.NewReader(exact)); err != nil {
		t.Fatalf("Put() at exactly MaxBytes should succeed, got error = %v", err)
	}
}

func TestLocalDiskStore_DoesNotLeaveAPartialFileOnOversizedUpload(t *testing.T) {
	store := newTestStore(t)
	oversized := bytes.Repeat([]byte("a"), int(store.MaxBytes)*2)

	key, err := store.Put(context.Background(), "user-1", "image/jpeg", bytes.NewReader(oversized))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Put() error = %v, want ErrTooLarge", err)
	}
	if key != "" {
		if _, statErr := os.Stat(filepath.Join(store.BaseDir, key)); statErr == nil {
			t.Fatalf("expected the oversized upload's partial file to be removed, but it still exists")
		}
	}
}

func TestLocalDiskStore_TwoUploadsFromTheSameUserGetDifferentKeys(t *testing.T) {
	store := newTestStore(t)

	key1, err := store.Put(context.Background(), "user-1", "image/jpeg", bytes.NewReader([]byte("first")))
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	key2, err := store.Put(context.Background(), "user-1", "image/jpeg", bytes.NewReader([]byte("second")))
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if key1 == key2 {
		t.Fatalf("expected distinct keys for two separate uploads, got the same key %q twice", key1)
	}
}
