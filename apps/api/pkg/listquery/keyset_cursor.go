package listquery

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// KeysetCursor identifies a keyset-pagination boundary: the last row's sort
// value (as its wire-format string — the caller parses it back into the
// column's native type before using it in SQL) plus its id as a tiebreaker.
// Backward marks a cursor that paginates toward earlier rows (Prev) rather
// than later ones (Next) — the same type serves both directions.
type KeysetCursor struct {
	SortValue string
	ID        uuid.UUID
	Backward  bool
}

// EncodeKeysetCursor returns a URL-safe cursor token.
func EncodeKeysetCursor(c KeysetCursor) string {
	dir := "f"
	if c.Backward {
		dir = "b"
	}
	payload := dir + "|" + c.SortValue + "|" + c.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// DecodeKeysetCursor parses a cursor token from the client.
func DecodeKeysetCursor(token string) (KeysetCursor, error) {
	if strings.TrimSpace(token) == "" {
		return KeysetCursor{}, fmt.Errorf("%w: empty cursor", ErrInvalidQuery)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return KeysetCursor{}, fmt.Errorf("%w: invalid cursor", ErrInvalidQuery)
	}
	parts := strings.SplitN(string(raw), "|", 3)
	if len(parts) != 3 {
		return KeysetCursor{}, fmt.Errorf("%w: invalid cursor payload", ErrInvalidQuery)
	}
	id, err := uuid.Parse(parts[2])
	if err != nil {
		return KeysetCursor{}, fmt.Errorf("%w: invalid cursor id", ErrInvalidQuery)
	}
	var backward bool
	switch parts[0] {
	case "f":
		backward = false
	case "b":
		backward = true
	default:
		return KeysetCursor{}, fmt.Errorf("%w: invalid cursor direction", ErrInvalidQuery)
	}
	return KeysetCursor{SortValue: parts[1], ID: id, Backward: backward}, nil
}

// KeysetClause builds the WHERE fragment and effective scan ORDER BY
// direction for keyset pagination against a single sort column plus id
// tiebreaker. sortValue must already be the column's native Go type (e.g.
// time.Time for a timestamptz column, a decimal string for a numeric
// column with an explicit cast) — decoding a KeysetCursor's wire-format
// SortValue string into that type is the caller's job, since only the
// caller knows the column's type.
//
// sortOrder is the client-requested order ("asc"/"desc"). For a Backward
// (Prev) cursor the effective scan direction is flipped from sortOrder —
// callers must use the returned scanOrderDir for their ORDER BY, run the
// query, and then reverse the resulting rows before use, so a Prev page is
// presented in the same visual order as a Next page.
//
// The comparison is always a strict "<" or ">" on the (sortCol, id) tuple —
// never "<=" or ">=" — which is what guarantees no row is ever skipped or
// repeated across a page boundary, even when many rows tie on sortCol.
func KeysetClause(sortValue any, id uuid.UUID, backward bool, sortCol, sortOrder string, startArgIdx int) (whereFrag string, scanOrderDir string, args []any) {
	wantsDesc := strings.EqualFold(sortOrder, "desc")
	if backward {
		wantsDesc = !wantsDesc
	}
	op := ">"
	scanOrderDir = "ASC"
	if wantsDesc {
		op = "<"
		scanOrderDir = "DESC"
	}
	n1, n2 := startArgIdx+1, startArgIdx+2
	whereFrag = fmt.Sprintf("(%s, id) %s ($%d, $%d)", sortCol, op, n1, n2) // #nosec G201 -- sortCol is caller-resolved from a schema allowlist, never client input; values are $N placeholders
	return whereFrag, scanOrderDir, []any{sortValue, id.String()}
}
