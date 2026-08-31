package rotation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/crypto"
)

// Rewrap rotation.
//
// Rotator (rotation.go) rotates by decrypting each row and re-encrypting it
// under the new key. That is the only option when ciphertext is sealed directly
// under the master key, but it makes rotation cost proportional to data volume
// and routes every plaintext through the rotator.
//
// RewrapRotator rotates envelope-encrypted rows instead. For each row it
// unwraps the per-record data key under its old master key version and rewraps
// it under the active one. The record ciphertext is never read, never rewritten,
// and the plaintext is never recovered — so rotation touches a fixed number of
// bytes per row regardless of how large the protected field is.
//
// This is what makes emergency master-key rotation a realistic incident
// response step rather than a multi-hour migration.

// WrappedRow is a single envelope-encrypted row awaiting rewrap.
//
// Note what is absent: the record ciphertext. The rewrap path has no need for
// it, so the store does not load it and the rotator cannot accidentally log it.
type WrappedRow struct {
	ID             uuid.UUID
	WrappedDataKey []byte
	KeyVersion     string
}

// WrapStore is the persistence surface the rewrap rotator needs.
//
// Like Store, it exposes only what a safe rotation requires, and nothing that
// returns plaintext.
type WrapStore interface {
	// CountPendingWraps reports how many rows are not yet on activeVersion.
	CountPendingWraps(ctx context.Context, activeVersion string) (int, error)
	// ScanPendingWraps returns up to limit rows whose key version is not
	// activeVersion.
	ScanPendingWraps(ctx context.Context, activeVersion string, limit int) ([]WrappedRow, error)
	// UpdateWrappedKey atomically replaces one row's wrapped data key and key
	// version. It must be a single statement or transaction: a row whose
	// version was updated without its wrapped key, or vice versa, is
	// permanently unreadable.
	UpdateWrappedKey(ctx context.Context, id uuid.UUID, wrappedDataKey []byte, keyVersion string) error
}

// Rewrapper is the subset of *crypto.EnvelopeCipher the rewrap rotator needs.
type Rewrapper interface {
	ActiveVersion() string
	Rewrap(rec crypto.SealedRecord) (crypto.SealedRecord, error)
}

// RewrapStats summarises a rewrap run.
type RewrapStats struct {
	// Pending is the count of rows needing rewrap observed at the start.
	Pending int
	// Rewrapped is the number of rows successfully rewrapped this run.
	Rewrapped int
	// Skipped counts rows that were already on the active version by the time
	// they were processed — a concurrent run or a retry got there first.
	Skipped int
}

// RewrapRotator moves wrapped data keys onto the active master key version.
type RewrapRotator struct {
	store  WrapStore
	cipher Rewrapper
}

// NewRewrapRotator constructs a rewrap rotator.
func NewRewrapRotator(store WrapStore, cipher Rewrapper) *RewrapRotator {
	return &RewrapRotator{store: store, cipher: cipher}
}

// ErrRewrapIncomplete reports that a run ended with rows still pending.
//
// It is returned rather than silently succeeding so that automation cannot
// mistake a partial run for a finished one and go on to retire an old key that
// rows still depend on.
var ErrRewrapIncomplete = errors.New("rewrap rotation ended with rows still pending")

// Run rewraps every row not already on the active master key version.
//
// The operational properties, each of which the incident runbook depends on:
//
//   - Idempotent: a second run finds nothing pending and does nothing. Rewrap
//     itself is a no-op for a row already on the active version, so even a
//     racing run cannot corrupt one.
//   - Resumable: each row is committed as it is rewrapped. An interrupted run
//     leaves completed rows on the active version, and a re-run continues from
//     where it stopped without repeating work.
//   - Safe under partial failure: a row that fails to rewrap stops the run with
//     its ID reported. Nothing is left half-updated, because the wrapped key
//     and its version are written in a single store operation.
//   - Observable: progress is logged per batch, and the returned stats let a
//     caller confirm the run actually finished.
func (r *RewrapRotator) Run(ctx context.Context, opts Options) (RewrapStats, error) {
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	active := r.cipher.ActiveVersion()

	pending, err := r.store.CountPendingWraps(ctx, active)
	if err != nil {
		return RewrapStats{}, fmt.Errorf("count pending wraps: %w", err)
	}
	stats := RewrapStats{Pending: pending}
	logger.Info("key rewrap starting",
		"active_version", active, "pending", pending, "batch_size", batchSize)

	for {
		if err := ctx.Err(); err != nil {
			// A cancelled context is not a failure of the rotation: the rows
			// already committed remain correctly rewrapped. The error is
			// returned so the caller knows the run did not finish.
			return stats, err
		}
		batch, err := r.store.ScanPendingWraps(ctx, active, batchSize)
		if err != nil {
			return stats, fmt.Errorf("scan pending wraps: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		for _, row := range batch {
			rewrapped, err := r.rewrapRow(ctx, row, active)
			if err != nil {
				return stats, err
			}
			if rewrapped {
				stats.Rewrapped++
			} else {
				stats.Skipped++
			}
		}
		logger.Info("key rewrap progress",
			"rewrapped", stats.Rewrapped, "skipped", stats.Skipped, "pending", stats.Pending)
	}

	// Confirm the rotation actually drained. Reporting success while rows
	// remain would be the precondition for retiring a key those rows still
	// need, which is the one irreversible mistake in this whole procedure.
	remaining, err := r.store.CountPendingWraps(ctx, active)
	if err != nil {
		return stats, fmt.Errorf("verify pending wraps: %w", err)
	}
	if remaining > 0 {
		logger.Warn("key rewrap finished with rows still pending",
			"remaining", remaining, "active_version", active)
		return stats, fmt.Errorf("%w: %d rows remain on an older key version", ErrRewrapIncomplete, remaining)
	}

	logger.Info("key rewrap complete",
		"rewrapped", stats.Rewrapped, "skipped", stats.Skipped, "active_version", active)
	return stats, nil
}

// rewrapRow rewraps one row, reporting whether it changed anything.
func (r *RewrapRotator) rewrapRow(ctx context.Context, row WrappedRow, active string) (bool, error) {
	if row.KeyVersion == active {
		// Already current. ScanPendingWraps filters these out, but a concurrent
		// run may have rewrapped it between the scan and now.
		return false, nil
	}

	rewrapped, err := r.cipher.Rewrap(crypto.SealedRecord{
		KeyVersion:     row.KeyVersion,
		WrappedDataKey: row.WrappedDataKey,
		// Ciphertext is deliberately empty: Rewrap does not read it, and not
		// loading it keeps the record data out of the rotation path entirely.
	})
	if err != nil {
		// The error carries only the row ID and key version — never the
		// wrapped key, the data key, or any record content.
		return false, fmt.Errorf("rewrap row %s (version %s): %w", row.ID, row.KeyVersion, err)
	}

	if err := r.store.UpdateWrappedKey(ctx, row.ID, rewrapped.WrappedDataKey, rewrapped.KeyVersion); err != nil {
		return false, fmt.Errorf("update wrapped key for row %s: %w", row.ID, err)
	}
	return true, nil
}

// VerifyRetirable reports whether a master key version can be safely retired.
//
// A version is retirable only when no stored row still references it. Calling
// this before removing a key from the configuration is the check that prevents
// the one unrecoverable failure in this design: retiring a key while rows still
// need it renders those rows permanently undecryptable, with no remedy short of
// restoring the key.
func VerifyRetirable(ctx context.Context, store WrapStore, versionToRetire, activeVersion string) error {
	if versionToRetire == activeVersion {
		return fmt.Errorf("refusing to retire %q: it is the active key version", versionToRetire)
	}
	pending, err := store.CountPendingWraps(ctx, activeVersion)
	if err != nil {
		return fmt.Errorf("count rows not on the active version: %w", err)
	}
	if pending > 0 {
		return fmt.Errorf(
			"refusing to retire %q: %d rows are not yet on the active version %q",
			versionToRetire, pending, activeVersion)
	}
	return nil
}
