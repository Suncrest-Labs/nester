package listquery_test

import (
	"bytes"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/pkg/listquery"
)

func TestKeysetCursorRoundTrip(t *testing.T) {
	id := uuid.New()
	c := listquery.KeysetCursor{SortValue: "2026-01-15T12:00:00Z", ID: id, Backward: true}
	token := listquery.EncodeKeysetCursor(c)
	decoded, err := listquery.DecodeKeysetCursor(token)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != c {
		t.Fatalf("cursor mismatch: got %+v want %+v", decoded, c)
	}
}

func TestSettlementCursorRoundTripStillWorks(t *testing.T) {
	// The deprecated wrapper must stay behaviorally identical now that it
	// delegates to KeysetCursor internally.
	id := uuid.New()
	created := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	token := listquery.EncodeSettlementCursor(created, id)
	decoded, err := listquery.DecodeSettlementCursor(token)
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.CreatedAt.Equal(created) || decoded.ID != id {
		t.Fatalf("cursor mismatch: %+v", decoded)
	}
}

// ksRow is a stand-in for a database row: a sort column plus its id
// tiebreaker, used to prove the keyset comparison rule KeysetClause encodes
// (strict "<"/">" on the (sortValue, id) tuple) is free of overlap/skip.
type ksRow struct {
	sortValue time.Time
	id        uuid.UUID
}

func tupleLess(a, b ksRow) bool {
	if !a.sortValue.Equal(b.sortValue) {
		return a.sortValue.Before(b.sortValue)
	}
	return bytes.Compare(a.id[:], b.id[:]) < 0
}

// simulatePage applies exactly the comparison rule KeysetClause documents —
// a strict "<"/">" on the (sortValue, id) tuple, in the scan direction
// KeysetClause itself returns — against an in-memory row set, standing in
// for what the real SQL WHERE/ORDER BY would do.
func simulatePage(rows []ksRow, cursor *listquery.KeysetCursor, sortOrder string, pageSize int) (page []ksRow, hasMore bool) {
	all := make([]ksRow, len(rows))
	copy(all, rows)

	scanOrderDir := "DESC"
	if sortOrder == "asc" {
		scanOrderDir = "ASC"
	}

	var filtered []ksRow
	if cursor == nil {
		filtered = all
	} else {
		cursorSortValue, err := time.Parse(time.RFC3339Nano, cursor.SortValue)
		if err != nil {
			panic(err)
		}
		cursorRow := ksRow{sortValue: cursorSortValue, id: cursor.ID}
		_, scanOrderDir, _ = listquery.KeysetClause(cursorSortValue, cursor.ID, cursor.Backward, "sort_col", sortOrder, 0)
		wantsDesc := scanOrderDir == "DESC"
		for _, r := range all {
			if wantsDesc && tupleLess(r, cursorRow) {
				filtered = append(filtered, r)
			} else if !wantsDesc && tupleLess(cursorRow, r) {
				filtered = append(filtered, r)
			}
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		if scanOrderDir == "DESC" {
			return tupleLess(filtered[j], filtered[i])
		}
		return tupleLess(filtered[i], filtered[j])
	})

	hasMore = len(filtered) > pageSize
	if hasMore {
		filtered = filtered[:pageSize]
	}

	result := make([]ksRow, len(filtered))
	copy(result, filtered)
	if cursor != nil && cursor.Backward {
		for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
			result[i], result[j] = result[j], result[i]
		}
	}
	return result, hasMore
}

func cursorFromRow(r ksRow, backward bool) *listquery.KeysetCursor {
	return &listquery.KeysetCursor{SortValue: r.sortValue.Format(time.RFC3339Nano), ID: r.id, Backward: backward}
}

func seedTiedRows() []ksRow {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Several timestamps repeat across rows so ties on the sort column are
	// unavoidable and must still be split correctly by the id tiebreaker.
	stamps := []time.Time{
		base, base, base,
		base.Add(time.Hour), base.Add(time.Hour),
		base.Add(2 * time.Hour), base.Add(2 * time.Hour),
	}
	rows := make([]ksRow, len(stamps))
	for i, ts := range stamps {
		rows[i] = ksRow{sortValue: ts, id: uuid.New()}
	}
	return rows
}

func canonicalDescOrder(rows []ksRow) []ksRow {
	out := make([]ksRow, len(rows))
	copy(out, rows)
	sort.Slice(out, func(i, j int) bool { return tupleLess(out[j], out[i]) })
	return out
}

// TestKeysetPaginationForwardWalkNoOverlapNoSkip walks the full dataset
// forward with a page size smaller than any tied group and asserts the
// concatenation of every page exactly reconstructs the full sorted set,
// with zero duplicates and zero omissions.
func TestKeysetPaginationForwardWalkNoOverlapNoSkip(t *testing.T) {
	rows := seedTiedRows()
	want := canonicalDescOrder(rows)

	var collected []ksRow
	var cursor *listquery.KeysetCursor
	for i := 0; i < 20; i++ { // hard cap: guards against an infinite loop on a bug
		page, hasMore := simulatePage(rows, cursor, "desc", 2)
		if len(page) == 0 {
			break
		}
		collected = append(collected, page...)
		if !hasMore {
			break
		}
		last := page[len(page)-1]
		cursor = cursorFromRow(last, false)
	}

	if len(collected) != len(rows) {
		t.Fatalf("forward walk collected %d rows, want %d", len(collected), len(rows))
	}
	seen := map[uuid.UUID]bool{}
	for _, r := range collected {
		if seen[r.id] {
			t.Fatalf("row %s returned twice during forward walk", r.id)
		}
		seen[r.id] = true
	}
	for i := range collected {
		if collected[i].id != want[i].id {
			t.Fatalf("forward walk order mismatch at index %d", i)
		}
	}
}

// TestKeysetPaginationBackwardReconstructsForwardPage proves the bidirectional
// guarantee directly: fetching a page forward, then asking for the page
// "before" its first-following row via a Backward cursor, reconstructs the
// exact same rows in the exact same order — across a tie boundary.
func TestKeysetPaginationBackwardReconstructsForwardPage(t *testing.T) {
	rows := seedTiedRows()
	want := canonicalDescOrder(rows)

	const pageSize = 2
	mid := 1 // the anchor (want[0]) shares its tie group with want[1]; the
	// backward cursor's anchor (want[mid+pageSize] = want[3]) shares its tie
	// group with want[2] — both directions cross a tied pair.

	forwardCursor := cursorFromRow(want[mid-1], false)
	page, _ := simulatePage(rows, forwardCursor, "desc", pageSize)
	if len(page) != pageSize {
		t.Fatalf("expected %d rows, got %d", pageSize, len(page))
	}
	for i := range page {
		if page[i].id != want[mid+i].id {
			t.Fatalf("forward page mismatch at %d", i)
		}
	}

	backwardCursor := cursorFromRow(want[mid+pageSize], true)
	backPage, _ := simulatePage(rows, backwardCursor, "desc", pageSize)
	if len(backPage) != len(page) {
		t.Fatalf("backward page length %d, want %d", len(backPage), len(page))
	}
	for i := range backPage {
		if backPage[i].id != page[i].id {
			t.Fatalf("backward page did not reconstruct forward page at %d: got %s want %s", i, backPage[i].id, page[i].id)
		}
	}
}
