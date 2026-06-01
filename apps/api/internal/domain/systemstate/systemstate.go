// Package systemstate defines the domain types and repository interface for the
// system_state key-value store.  Operational values that must survive process
// restarts (e.g. the event-indexer ledger cursor, feature flags) are kept here
// rather than scattered across one-off columns in domain tables.
package systemstate

import (
	"context"
	"errors"
)

// ErrKeyNotFound is returned by Get when the requested key does not exist in
// the store.  Callers should treat this as a typed sentinel rather than a
// hard error so they can supply a default value.
var ErrKeyNotFound = errors.New("system_state: key not found")

// Well-known keys used by the application.
const (
	// KeyLastLedger is the event-indexer cursor: the last Stellar ledger
	// sequence that was successfully processed.
	KeyLastLedger = "event_indexer.last_ledger"
)

// Repository is the persistence interface for the system_state table.
type Repository interface {
	// Get returns the value stored under key.  If the key does not exist it
	// returns ("", ErrKeyNotFound).
	Get(ctx context.Context, key string) (string, error)

	// Set upserts the value for key, updating updated_at to NOW().
	Set(ctx context.Context, key string, value string) error
}
