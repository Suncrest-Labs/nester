// Package export implements server-side generation of complete,
// authoritative data exports (transaction-history CSV, account statements)
// from the platform's source of truth.
//
// Correctness guarantees:
//   - Completeness: every movement in range is included — no pagination
//     gaps, no filtered-away rows.
//   - Reconciliation: the sum of exported movements must equal the net
//     balance change the ledger reports for the same period; on mismatch
//     the export FAILS rather than delivering a wrong document.
//   - Security: generated files are delivered via time-limited,
//     ownership-verified tokens (see token.go), never guessable URLs.
//
// Large exports route to the durable job queue above AsyncRowThreshold so a
// year-of-history request never blocks a request worker or times out.
package export

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"time"

	"github.com/shopspring/decimal"
)

// AsyncRowThreshold is the documented size threshold: exports whose row
// count exceeds it run asynchronously on the job queue; smaller exports may
// complete inline for responsiveness.
const AsyncRowThreshold = 5000

// Movement is a single ledger movement included in an export: deposits,
// withdrawals, harvests and fees all appear — every movement, or the export
// does not ship.
type Movement struct {
	ID        string
	Timestamp time.Time
	Kind      string // deposit | withdrawal | harvest | fee
	Asset     string
	// Amount is signed: inflows positive, outflows negative, so the column
	// sums to the period's net change.
	Amount   decimal.Decimal
	Decimals int32 // asset display decimals
	VaultID  string
	Memo     string
}

// Source supplies export data from the server-side source of truth.
type Source interface {
	// Movements returns every movement for the user in [from, to), oldest
	// first, with no pagination gap.
	Movements(ctx context.Context, userID string, from, to time.Time) ([]Movement, error)
	// NetChange returns the ledger's authoritative net balance change per
	// asset over the same period. Used to reconcile the export.
	NetChange(ctx context.Context, userID string, from, to time.Time) (map[string]decimal.Decimal, error)
}

// ErrReconciliation is returned when exported movements do not sum to the
// ledger's reported net change. The export is withheld: a statement that
// disagrees with the platform's own accounting must never be delivered.
type ErrReconciliation struct {
	Asset    string
	Exported decimal.Decimal
	Ledger   decimal.Decimal
}

func (e *ErrReconciliation) Error() string {
	return fmt.Sprintf("export: reconciliation failed for %s: exported sum %s != ledger net change %s",
		e.Asset, e.Exported.String(), e.Ledger.String())
}

// CSVColumns is the stable, documented column schema for transaction-history
// exports. Integrations may rely on this order; append new columns at the
// end, never reorder or remove.
var CSVColumns = []string{"id", "timestamp_utc", "kind", "asset", "amount", "vault_id", "memo"}

// Request describes one export request.
type Request struct {
	UserID string
	From   time.Time
	To     time.Time
	Format string // "csv" (transaction history) | "pdf" (statement)
}

// JobEnqueuer routes large exports to the durable job queue.
type JobEnqueuer interface {
	EnqueueExport(ctx context.Context, req Request) (jobID string, err error)
}

// Service generates exports and decides inline-vs-async routing.
type Service struct {
	source   Source
	jobs     JobEnqueuer
	rowLimit int
}

// NewService builds an export Service. rowLimit <= 0 uses AsyncRowThreshold.
func NewService(source Source, jobs JobEnqueuer, rowLimit int) *Service {
	if rowLimit <= 0 {
		rowLimit = AsyncRowThreshold
	}
	return &Service{source: source, jobs: jobs, rowLimit: rowLimit}
}

// Result is the outcome of an export request: either the inline document or
// the job id of the queued asynchronous generation.
type Result struct {
	Async bool
	JobID string
}

// Generate produces the export for req. Exports larger than the row
// threshold are enqueued on the durable job queue and generate in the
// background (Result.Async true); smaller exports are written to w inline.
func (s *Service) Generate(ctx context.Context, w io.Writer, req Request) (Result, error) {
	movements, err := s.source.Movements(ctx, req.UserID, req.From, req.To)
	if err != nil {
		return Result{}, fmt.Errorf("export: load movements: %w", err)
	}

	if len(movements) > s.rowLimit {
		if s.jobs == nil {
			return Result{}, fmt.Errorf("export: %d rows exceeds inline threshold %d and no job queue configured", len(movements), s.rowLimit)
		}
		jobID, err := s.jobs.EnqueueExport(ctx, req)
		if err != nil {
			return Result{}, fmt.Errorf("export: enqueue: %w", err)
		}
		return Result{Async: true, JobID: jobID}, nil
	}

	if err := s.writeVerified(ctx, w, req, movements); err != nil {
		return Result{}, err
	}
	return Result{Async: false}, nil
}

// writeVerified reconciles then writes — never the other way around, so a
// document that fails reconciliation is never partially delivered.
func (s *Service) writeVerified(ctx context.Context, w io.Writer, req Request, movements []Movement) error {
	if err := s.reconcile(ctx, req, movements); err != nil {
		return err
	}
	return WriteCSV(w, movements)
}

// reconcile asserts the completeness invariant: per asset, the sum of
// exported movements equals the ledger's net change for the period.
func (s *Service) reconcile(ctx context.Context, req Request, movements []Movement) error {
	ledger, err := s.source.NetChange(ctx, req.UserID, req.From, req.To)
	if err != nil {
		return fmt.Errorf("export: load ledger net change: %w", err)
	}

	sums := make(map[string]decimal.Decimal)
	for _, m := range movements {
		sums[m.Asset] = sums[m.Asset].Add(m.Amount)
	}

	for asset, ledgerNet := range ledger {
		if !sums[asset].Equal(ledgerNet) {
			return &ErrReconciliation{Asset: asset, Exported: sums[asset], Ledger: ledgerNet}
		}
	}
	for asset, sum := range sums {
		if _, ok := ledger[asset]; !ok && !sum.IsZero() {
			return &ErrReconciliation{Asset: asset, Exported: sum, Ledger: decimal.Zero}
		}
	}
	return nil
}

// WriteCSV writes movements in the stable CSVColumns schema. Amounts are
// rendered with the asset's display decimals; timestamps are UTC RFC 3339 so
// the document is unambiguous regardless of reader locale.
func WriteCSV(w io.Writer, movements []Movement) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(CSVColumns); err != nil {
		return fmt.Errorf("export: write header: %w", err)
	}
	for _, m := range movements {
		rec := []string{
			m.ID,
			m.Timestamp.UTC().Format(time.RFC3339),
			m.Kind,
			m.Asset,
			m.Amount.StringFixed(m.Decimals),
			m.VaultID,
			m.Memo,
		}
		if err := cw.Write(rec); err != nil {
			return fmt.Errorf("export: write row %s: %w", m.ID, err)
		}
	}
	cw.Flush()
	return cw.Error()
}
