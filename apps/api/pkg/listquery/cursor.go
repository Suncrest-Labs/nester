package listquery

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SettlementCursor identifies the last row of a page for keyset pagination.
//
// Deprecated: this is a thin, forward-only wrapper kept for source
// compatibility. New code should use KeysetCursor directly, which also
// supports backward (Prev) pagination.
type SettlementCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// EncodeSettlementCursor returns a URL-safe cursor token.
//
// Deprecated: use EncodeKeysetCursor.
func EncodeSettlementCursor(createdAt time.Time, id uuid.UUID) string {
	return EncodeKeysetCursor(KeysetCursor{
		SortValue: createdAt.UTC().Format(time.RFC3339Nano),
		ID:        id,
	})
}

// DecodeSettlementCursor parses a cursor token from the client.
//
// Deprecated: use DecodeKeysetCursor.
func DecodeSettlementCursor(token string) (SettlementCursor, error) {
	kc, err := DecodeKeysetCursor(token)
	if err != nil {
		return SettlementCursor{}, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, kc.SortValue)
	if err != nil {
		return SettlementCursor{}, fmt.Errorf("%w: invalid cursor timestamp", ErrInvalidQuery)
	}
	return SettlementCursor{CreatedAt: createdAt.UTC(), ID: kc.ID}, nil
}
