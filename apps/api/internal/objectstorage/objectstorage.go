// Package objectstorage stores uploaded files (currently: KYC identity
// documents) behind a small interface, so the handler that accepts an
// upload never has to know whether the bytes land on local disk, S3, or
// anywhere else.
//
// Before this package existed, the KYC upload handler never uploaded
// anything: it synthesized a storage key from the client-supplied filename
// and discarded the file bytes entirely (nester#1191). This is a real,
// working implementation — not a mock — but it is deliberately a local-disk
// store rather than a cloud client: no S3/GCS credential, bucket, or SDK
// decision has been made for this repo yet (confirmed: no AWS/GCS SDK
// dependency exists in go.mod). The issue's own guidance is explicit for
// this situation — "If storage is not ready, the endpoint should reject
// document uploads rather than accept and discard them" — so Store's
// contract is designed so a production cloud implementation is a drop-in
// Store replacement later, and the handler treats a nil Store the same as
// "storage is not ready" (503), never as "skip the upload."
package objectstorage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrContentTypeNotAllowed is returned when the upload's declared
	// content type is not in the caller-supplied allowlist.
	ErrContentTypeNotAllowed = errors.New("content type not allowed")
	// ErrTooLarge is returned when the upload exceeds MaxBytes.
	ErrTooLarge = errors.New("upload exceeds maximum allowed size")
)

// Store persists uploaded file content and returns a storage key a later
// read can use to retrieve it. The key is never derived from anything the
// caller supplies (nester#1191's second acceptance criterion) — Store
// generates it itself.
type Store interface {
	// Put reads up to MaxBytes from r, validates contentType against
	// allowedContentTypes, and persists the content. keyPrefix scopes the
	// generated key (e.g. to a user id) without letting the caller choose
	// the key itself. Returns the server-generated key on success.
	Put(ctx context.Context, keyPrefix string, contentType string, r io.Reader) (key string, err error)

	// Delete removes the object previously stored under key. It is used as
	// compensating cleanup when a later step in a multi-object upload flow
	// fails after some objects have already been persisted (e.g. id_front
	// stored but id_back rejected, or the subsequent domain call fails) —
	// Store has no transaction to roll back, so the caller calls Delete
	// itself to avoid leaving an orphaned, unreferenced object behind.
	// Deleting a key that does not exist is not an error.
	Delete(ctx context.Context, key string) error
}

// LocalDiskStore is a real, working Store backed by the local filesystem.
// Suitable for local development and as a functioning default in
// environments that haven't provisioned cloud object storage yet — not
// intended as the production store for a multi-instance deployment, where
// a shared bucket (S3/GCS) is the right choice once that decision is made.
type LocalDiskStore struct {
	// BaseDir is the root directory files are written under. Created if it
	// does not already exist.
	BaseDir string
	// MaxBytes bounds how much of r is read, regardless of any
	// Content-Length the caller claims. A caller that provides more than
	// MaxBytes fails with ErrTooLarge rather than exhausting disk.
	MaxBytes int64
	// AllowedContentTypes is the exact set of content types Put will
	// accept. Anything else is rejected with ErrContentTypeNotAllowed
	// before any bytes are written.
	AllowedContentTypes map[string]struct{}
}

// NewLocalDiskStore constructs a LocalDiskStore and ensures baseDir exists.
func NewLocalDiskStore(baseDir string, maxBytes int64, allowedContentTypes []string) (*LocalDiskStore, error) {
	if err := os.MkdirAll(baseDir, 0o750); err != nil {
		return nil, fmt.Errorf("objectstorage: create base dir: %w", err)
	}
	allowed := make(map[string]struct{}, len(allowedContentTypes))
	for _, ct := range allowedContentTypes {
		allowed[ct] = struct{}{}
	}
	return &LocalDiskStore{BaseDir: baseDir, MaxBytes: maxBytes, AllowedContentTypes: allowed}, nil
}

func (s *LocalDiskStore) Put(_ context.Context, keyPrefix string, contentType string, r io.Reader) (string, error) {
	if _, ok := s.AllowedContentTypes[contentType]; !ok {
		return "", fmt.Errorf("%w: %q", ErrContentTypeNotAllowed, contentType)
	}

	key, err := generateKey(keyPrefix)
	if err != nil {
		return "", fmt.Errorf("objectstorage: generate key: %w", err)
	}

	destPath := filepath.Join(s.BaseDir, key)
	// 0o700 for the same reason as the file mode below — no other account
	// needs to traverse the KYC document tree.
	if err := os.MkdirAll(filepath.Dir(destPath), 0o700); err != nil {
		return "", fmt.Errorf("objectstorage: create key dir: %w", err)
	}

	// #nosec G304 -- destPath is built entirely from generateKey's own
	// output (a fresh UUID-shaped random hex string) and keyPrefix, which
	// callers pass as a trusted internal value (a user id), never from
	// unsanitized client input — see generateKey's own doc comment.
	// 0o600, not 0o640: these are KYC identity documents, and nothing but
	// this process needs to read them. A group-readable mode widens access
	// to every account in the service's group for no benefit, which is what
	// gosec's G302 flags here.
	f, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("objectstorage: open destination: %w", err)
	}
	defer f.Close()

	limited := io.LimitReader(r, s.MaxBytes+1)
	written, err := io.Copy(f, limited)
	if err != nil {
		_ = os.Remove(destPath)
		return "", fmt.Errorf("objectstorage: write: %w", err)
	}
	if written > s.MaxBytes {
		_ = os.Remove(destPath)
		return "", ErrTooLarge
	}

	return key, nil
}

// Delete removes the object stored at key. A missing file is not treated as
// an error: the caller is cleaning up best-effort, and the object being
// already gone satisfies that goal just as well as removing it now.
//
// #nosec G304 -- key originates only from this store's own generateKey
// output, embedded in a caller-held storage key string (e.g. persisted on a
// KYC record or passed straight back from Put) — never derived from
// unsanitized client input.
func (s *LocalDiskStore) Delete(_ context.Context, key string) error {
	destPath := filepath.Join(s.BaseDir, key)
	if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("objectstorage: delete: %w", err)
	}
	return nil
}

// generateKey builds a storage key from keyPrefix and a fresh random
// component — never from anything a client supplies (e.g. an uploaded
// file's declared filename), so a hostile filename (path separators,
// traversal sequences, an attempt to overwrite another user's key) cannot
// influence what gets stored or where (nester#1191).
func generateKey(keyPrefix string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	safePrefix := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			return r
		default:
			return '_'
		}
	}, keyPrefix)
	return filepath.Join(safePrefix, hex.EncodeToString(buf)), nil
}
