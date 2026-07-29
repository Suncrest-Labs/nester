package export

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// fakeSource seeds movements and an independently-stated ledger net change,
// so tests can make them agree (happy path) or deliberately disagree
// (reconciliation failure).
type fakeSource struct {
	movements []Movement
	netChange map[string]decimal.Decimal
}

func (f *fakeSource) Movements(context.Context, string, time.Time, time.Time) ([]Movement, error) {
	return f.movements, nil
}

func (f *fakeSource) NetChange(context.Context, string, time.Time, time.Time) (map[string]decimal.Decimal, error) {
	return f.netChange, nil
}

type fakeQueue struct {
	enqueued []Request
}

func (f *fakeQueue) EnqueueExport(_ context.Context, req Request) (string, error) {
	f.enqueued = append(f.enqueued, req)
	return fmt.Sprintf("job-%d", len(f.enqueued)), nil
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func seedMovements(n int) ([]Movement, map[string]decimal.Decimal) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var ms []Movement
	total := decimal.Zero
	for i := 0; i < n; i++ {
		amt := dec("10.5")
		if i%4 == 3 { // every fourth movement is a fee (outflow)
			amt = dec("-0.25")
		}
		total = total.Add(amt)
		ms = append(ms, Movement{
			ID:        fmt.Sprintf("tx-%d", i),
			Timestamp: base.Add(time.Duration(i) * time.Hour),
			Kind:      "deposit",
			Asset:     "USDC",
			Amount:    amt,
			Decimals:  2,
			VaultID:   "vault-1",
		})
	}
	return ms, map[string]decimal.Decimal{"USDC": total}
}

// TestExportCompleteAndReconciled proves completeness: every seeded row
// appears in the CSV, and the exported sum matches the ledger.
func TestExportCompleteAndReconciled(t *testing.T) {
	movements, ledger := seedMovements(40)
	svc := NewService(&fakeSource{movements: movements, netChange: ledger}, nil, 100)

	var buf bytes.Buffer
	res, err := svc.Generate(context.Background(), &buf, Request{
		UserID: "u1",
		From:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		To:     time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		Format: "csv",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if res.Async {
		t.Fatal("small export should complete inline")
	}

	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if got, want := len(records), len(movements)+1; got != want { // +1 header
		t.Fatalf("csv rows = %d, want %d (header + every movement, no gaps)", got, want)
	}
	if got := strings.Join(records[0], ","); got != strings.Join(CSVColumns, ",") {
		t.Fatalf("header = %q, want stable schema %q", got, strings.Join(CSVColumns, ","))
	}

	// Sum the amount column and compare against the ledger — the same check
	// an accountant would do.
	sum := decimal.Zero
	for _, rec := range records[1:] {
		sum = sum.Add(dec(rec[4]))
	}
	if !sum.Equal(ledger["USDC"]) {
		t.Fatalf("csv amount sum %s != ledger net change %s", sum, ledger["USDC"])
	}
}

// TestExportFailsLoudlyOnReconciliationMismatch seeds a deliberate mismatch
// between movements and the ledger and asserts the export ERRORS rather than
// delivering a wrong document — the acceptance criterion.
func TestExportFailsLoudlyOnReconciliationMismatch(t *testing.T) {
	movements, ledger := seedMovements(10)
	// The ledger says one number; the movement rows sum to another
	// (simulating a silently-dropped row or a double post).
	ledger["USDC"] = ledger["USDC"].Add(dec("100"))

	svc := NewService(&fakeSource{movements: movements, netChange: ledger}, nil, 100)

	var buf bytes.Buffer
	_, err := svc.Generate(context.Background(), &buf, Request{UserID: "u1", Format: "csv"})

	var recErr *ErrReconciliation
	if !errors.As(err, &recErr) {
		t.Fatalf("want ErrReconciliation, got %v", err)
	}
	if buf.Len() != 0 {
		t.Fatal("a document failing reconciliation must not be delivered, even partially")
	}
}

// TestExportUnknownAssetInMovementsFailsReconciliation covers the inverse
// direction: movements contain an asset the ledger reports nothing for.
func TestExportUnknownAssetInMovementsFailsReconciliation(t *testing.T) {
	movements, ledger := seedMovements(4)
	movements = append(movements, Movement{
		ID: "tx-ghost", Timestamp: time.Now(), Kind: "deposit",
		Asset: "GHOST", Amount: dec("5"), Decimals: 2,
	})
	svc := NewService(&fakeSource{movements: movements, netChange: ledger}, nil, 100)

	var buf bytes.Buffer
	_, err := svc.Generate(context.Background(), &buf, Request{UserID: "u1", Format: "csv"})
	var recErr *ErrReconciliation
	if !errors.As(err, &recErr) {
		t.Fatalf("want ErrReconciliation for unledgered asset, got %v", err)
	}
}

// TestLargeExportRoutesToJobQueue: above the documented threshold the export
// is enqueued and returns a job id immediately, never generated inline.
func TestLargeExportRoutesToJobQueue(t *testing.T) {
	movements, ledger := seedMovements(50)
	queue := &fakeQueue{}
	svc := NewService(&fakeSource{movements: movements, netChange: ledger}, queue, 20)

	var buf bytes.Buffer
	res, err := svc.Generate(context.Background(), &buf, Request{UserID: "u1", Format: "csv"})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !res.Async || res.JobID == "" {
		t.Fatalf("want async result with job id, got %+v", res)
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(queue.enqueued))
	}
	if buf.Len() != 0 {
		t.Fatal("async export must not write inline output")
	}
}

// --- secure download tokens -------------------------------------------------

func testIssuer(t *testing.T, ttl time.Duration) *TokenIssuer {
	t.Helper()
	iss, err := NewTokenIssuer([]byte("0123456789abcdef0123456789abcdef"), ttl)
	if err != nil {
		t.Fatal(err)
	}
	return iss
}

// TestDownloadTokenOwnership proves the acceptance criterion: one user
// cannot download another user's export, even with a perfectly valid link.
func TestDownloadTokenOwnership(t *testing.T) {
	iss := testIssuer(t, time.Hour)
	token := iss.Issue("export-42", "alice")

	// The owner downloads fine.
	exportID, err := iss.Verify(token, "alice")
	if err != nil {
		t.Fatalf("owner verify: %v", err)
	}
	if exportID != "export-42" {
		t.Fatalf("exportID = %q, want export-42", exportID)
	}

	// Another authenticated user presenting the same link is refused.
	if _, err := iss.Verify(token, "mallory"); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("want ErrNotOwner for non-owner, got %v", err)
	}
}

func TestDownloadTokenExpiry(t *testing.T) {
	iss := testIssuer(t, time.Minute)
	token := iss.Issue("export-7", "alice")

	// Move the clock past the window.
	iss.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if _, err := iss.Verify(token, "alice"); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("want ErrTokenExpired, got %v", err)
	}
}

func TestDownloadTokenTamperingRejected(t *testing.T) {
	iss := testIssuer(t, time.Hour)
	token := iss.Issue("export-1", "alice")

	// Attacker rewrites the export id to fetch someone else's file.
	tampered := strings.Replace(token, "export-1", "export-2", 1)
	if _, err := iss.Verify(tampered, "alice"); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("want ErrTokenInvalid for tampered token, got %v", err)
	}

	if _, err := iss.Verify("not|a|token", "alice"); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("want ErrTokenInvalid for malformed token, got %v", err)
	}
}
