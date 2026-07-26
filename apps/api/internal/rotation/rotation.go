// Package rotation re-encrypts stored account ciphertext under the active key
// version. It is the migration engine behind envelope key rotation: legacy rows
// are decrypted with their recorded key version and re-sealed with the active
// key, one committed row at a time so the process is idempotent and resumable.
package rotation

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/crypto"
)

// EncryptedRow is a single stored ciphertext awaiting rotation.
type EncryptedRow struct {
	ID         uuid.UUID
	Ciphertext []byte
	KeyVersion string
}

// Store is the persistence surface the rotator needs. It deliberately exposes
// only the operations required for a safe, resumable rotation and nothing that
// would return plaintext.
type Store interface {
	// CountPending reports how many rows are not yet on activeVersion.
	CountPending(ctx context.Context, activeVersion string) (int, error)
	// ScanPending returns up to limit rows whose key version is not activeVersion.
	ScanPending(ctx context.Context, activeVersion string, limit int) ([]EncryptedRow, error)
	// UpdateCipher atomically replaces a single row's ciphertext and key version.
	UpdateCipher(ctx context.Context, id uuid.UUID, ciphertext []byte, keyVersion string) error
}

// Cipher is the subset of *crypto.AccountCipher the rotator relies on.
type Cipher interface {
	ActiveVersion() string
	Encrypt(plaintext string) (crypto.CipherEnvelope, error)
	Decrypt(env crypto.CipherEnvelope) (string, error)
}

// Stats summarizes a rotation run.
type Stats struct {
	// Pending is the count of rows needing rotation observed at the start.
	Pending int
	// Rotated is the number of rows successfully re-encrypted this run.
	Rotated int
}

// Options configures a rotation run.
type Options struct {
	// BatchSize bounds how many rows are loaded and re-encrypted per iteration.
	BatchSize int
	// Logger receives progress logs. Only counts and row IDs are logged — never
	// plaintext, keys, or ciphertext.
	Logger *slog.Logger
}

const defaultBatchSize = 500

// Rotator re-encrypts pending rows under the cipher's active key version.
type Rotator struct {
	store  Store
	cipher Cipher
}

// NewRotator constructs a Rotator over the given store and cipher.
func NewRotator(store Store, cipher Cipher) *Rotator {
	return &Rotator{store: store, cipher: cipher}
}

// Run rotates every row not already on the active key version.
//
//   - Idempotent: a second run finds no pending rows and does nothing.
//   - Resumable: each row is committed as it is rotated, so an interrupted run
//     leaves completed rows on the active version and a re-run continues with the
//     remainder.
//   - Safe: rows are processed in bounded batches, and any decrypt/update error
//     stops the run (surfacing only the row ID and version) rather than looping.
func (r *Rotator) Run(ctx context.Context, opts Options) (Stats, error) {
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	active := r.cipher.ActiveVersion()

	pending, err := r.store.CountPending(ctx, active)
	if err != nil {
		return Stats{}, fmt.Errorf("count pending: %w", err)
	}
	stats := Stats{Pending: pending}
	logger.Info("key rotation starting",
		"active_version", active, "pending", pending, "batch_size", batchSize)

	for {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		batch, err := r.store.ScanPending(ctx, active, batchSize)
		if err != nil {
			return stats, fmt.Errorf("scan pending: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		for _, row := range batch {
			if err := r.rotateRow(ctx, row, active); err != nil {
				return stats, err
			}
			stats.Rotated++
		}
		logger.Info("key rotation progress", "rotated", stats.Rotated, "pending", stats.Pending)
	}

	logger.Info("key rotation complete", "rotated", stats.Rotated, "active_version", active)
	return stats, nil
}

func (r *Rotator) rotateRow(ctx context.Context, row EncryptedRow, active string) error {
	// Belt-and-suspenders: ScanPending already filters these out, but never
	// re-encrypt a row that is already current.
	if row.KeyVersion == active {
		return nil
	}
	plaintext, err := r.cipher.Decrypt(crypto.CipherEnvelope{KeyVersion: row.KeyVersion, Ciphertext: row.Ciphertext})
	if err != nil {
		// The error intentionally carries only safe identifiers (row ID and key
		// version) — never the ciphertext or plaintext.
		return fmt.Errorf("decrypt row %s (version %s): %w", row.ID, row.KeyVersion, err)
	}
	env, err := r.cipher.Encrypt(plaintext)
	if err != nil {
		return fmt.Errorf("re-encrypt row %s: %w", row.ID, err)
	}
	if err := r.store.UpdateCipher(ctx, row.ID, env.Ciphertext, env.KeyVersion); err != nil {
		return fmt.Errorf("update row %s: %w", row.ID, err)
	}
	return nil
}
