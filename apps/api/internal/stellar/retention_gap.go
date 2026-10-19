package stellar

import (
	"fmt"
	"regexp"
	"strconv"
)

// CursorBehindRetentionError reports that the indexer's cursor is older than
// the oldest ledger the Soroban RPC still retains, so the events between the
// cursor and Oldest are gone from the node and can no longer be fetched by
// polling.
//
// It is a distinct type, not a generic fetch failure, because the remedy is
// different: retrying never helps, and jumping the cursor forward would
// silently skip real deposits and withdrawals. The cursor is left where it is
// and the gap is closed with cmd/backfill (or an archive-backed RPC).
type CursorBehindRetentionError struct {
	// Cursor is the ledger the indexer asked to resume from.
	Cursor uint64
	// Oldest is the oldest ledger the RPC currently serves.
	Oldest uint64
}

func (e *CursorBehindRetentionError) Error() string {
	return fmt.Sprintf(
		"indexer cursor ledger %d is older than the RPC retention window (oldest available ledger %d): "+
			"events in between cannot be polled; run cmd/backfill from ledger %d or use an archive RPC",
		e.Cursor, e.Oldest, e.Cursor,
	)
}

// rpcLedgerRange matches the RPC's rejection of an out-of-range startLedger,
// e.g. "startLedger must be within the ledger range: 5000 - 9000".
var rpcLedgerRange = regexp.MustCompile(`ledger range: (\d+) - (\d+)`)

// retentionGapFromRPCError classifies an RPC error message. It returns a
// typed error only when startLedger is below the oldest retained ledger; a
// cursor ahead of the range, or an unrecognised message, is not a retention
// gap and stays a generic error.
func retentionGapFromRPCError(message string, startLedger uint64) *CursorBehindRetentionError {
	m := rpcLedgerRange.FindStringSubmatch(message)
	if m == nil {
		return nil
	}
	oldest, err := strconv.ParseUint(m[1], 10, 64)
	if err != nil || startLedger >= oldest {
		return nil
	}
	return &CursorBehindRetentionError{Cursor: startLedger, Oldest: oldest}
}
