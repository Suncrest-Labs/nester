package listquery

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SettlementCursor identifies the last row of a page for keyset pagination.
type SettlementCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// EncodeSettlementCursor returns a URL-safe cursor token.
func EncodeSettlementCursor(createdAt time.Time, id uuid.UUID) string {
	payload := createdAt.UTC().Format(time.RFC3339Nano) + "|" + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// DecodeSettlementCursor parses a cursor token from the client.
func DecodeSettlementCursor(token string) (SettlementCursor, error) {
	if strings.TrimSpace(token) == "" {
		return SettlementCursor{}, fmt.Errorf("%w: empty cursor", ErrInvalidQuery)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return SettlementCursor{}, fmt.Errorf("%w: invalid cursor", ErrInvalidQuery)
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return SettlementCursor{}, fmt.Errorf("%w: invalid cursor payload", ErrInvalidQuery)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return SettlementCursor{}, fmt.Errorf("%w: invalid cursor timestamp", ErrInvalidQuery)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return SettlementCursor{}, fmt.Errorf("%w: invalid cursor id", ErrInvalidQuery)
	}
	return SettlementCursor{CreatedAt: createdAt.UTC(), ID: id}, nil
}
