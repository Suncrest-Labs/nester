package valuation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/portfolio"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
)

// fakeTxLister is an in-memory TxLister that honors Limit/Offset like the
// real postgres-backed implementation: it returns a genuine total count
// independent of the page size, so a caller that pages via that count
// eventually terminates and collects every row.
type fakeTxLister struct {
	txs []transaction.Transaction
	// calls records each (Limit, Offset) pair PendingDeposits requested, so
	// tests can assert on the exact paging behavior, not just the result.
	calls []transaction.ListFilter
}

func (f *fakeTxLister) ListUserTransactions(ctx context.Context, filter transaction.ListFilter) ([]transaction.Transaction, int, error) {
	f.calls = append(f.calls, filter)
	total := len(f.txs)
	start := filter.Offset
	if start > total {
		start = total
	}
	end := start + filter.Limit
	if end > total {
		end = total
	}
	return f.txs[start:end], total, nil
}

func makePendingDeposit(currency string, amount int64) transaction.Transaction {
	return transaction.Transaction{
		ID:       uuid.New(),
		Type:     transaction.TypeDeposit,
		Status:   transaction.StatusPending,
		Currency: currency,
		Amount:   decimal.NewFromInt(amount),
	}
}

func TestTxPendingSource_PendingDeposits_ReturnsAllRowsAcrossPages(t *testing.T) {
	// More rows than a single page (pendingDepositsPageSize) would return,
	// so this only passes if PendingDeposits actually pages rather than
	// truncating at the first response (nester#1193).
	var txs []transaction.Transaction
	rowCount := pendingDepositsPageSize*2 + 37
	for i := 0; i < rowCount; i++ {
		txs = append(txs, makePendingDeposit("USDC", 10))
	}
	lister := &fakeTxLister{txs: txs}
	source := NewTxPendingSource(lister)

	deposits, err := source.PendingDeposits(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("PendingDeposits() error = %v", err)
	}
	if len(deposits) != rowCount {
		t.Fatalf("got %d deposits, want %d (some rows were silently dropped)", len(deposits), rowCount)
	}

	if len(lister.calls) < 3 {
		t.Fatalf("expected at least 3 paged calls for %d rows at page size %d, got %d calls", rowCount, pendingDepositsPageSize, len(lister.calls))
	}
	for i, call := range lister.calls {
		if call.Limit != pendingDepositsPageSize {
			t.Fatalf("call %d: Limit = %d, want %d", i, call.Limit, pendingDepositsPageSize)
		}
	}
}

func TestTxPendingSource_PendingDeposits_SumsCorrectlyAcrossPages(t *testing.T) {
	lister := &fakeTxLister{txs: []transaction.Transaction{
		makePendingDeposit("USDC", 100),
		makePendingDeposit("USDC", 250),
		makePendingDeposit("XLM", 5),
	}}
	source := NewTxPendingSource(lister)

	deposits, err := source.PendingDeposits(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("PendingDeposits() error = %v", err)
	}

	var totalUSDC decimal.Decimal
	for _, d := range deposits {
		if d.Asset == "USDC" {
			totalUSDC = totalUSDC.Add(d.Amount)
		}
	}
	if want := decimal.NewFromInt(350); !totalUSDC.Equal(want) {
		t.Fatalf("total USDC = %s, want %s — this is exactly the undercount nester#1193 is about", totalUSDC, want)
	}
}

func TestTxPendingSource_PendingDeposits_EmptyResultNoRequests(t *testing.T) {
	lister := &fakeTxLister{}
	source := NewTxPendingSource(lister)

	deposits, err := source.PendingDeposits(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("PendingDeposits() error = %v", err)
	}
	if len(deposits) != 0 {
		t.Fatalf("got %d deposits, want 0", len(deposits))
	}
	if len(lister.calls) != 1 {
		t.Fatalf("expected exactly one call for an empty result, got %d", len(lister.calls))
	}
}

func TestTxPendingSource_PendingDeposits_SinglePageDoesNotOverFetch(t *testing.T) {
	lister := &fakeTxLister{txs: []transaction.Transaction{
		makePendingDeposit("USDC", 10),
		makePendingDeposit("USDC", 20),
	}}
	source := NewTxPendingSource(lister)

	if _, err := source.PendingDeposits(context.Background(), uuid.New()); err != nil {
		t.Fatalf("PendingDeposits() error = %v", err)
	}
	if len(lister.calls) != 1 {
		t.Fatalf("expected exactly one call when everything fits on one page, got %d", len(lister.calls))
	}
}

func TestTxPendingSource_PendingDeposits_PropagatesListerError(t *testing.T) {
	wantErr := errors.New("db unavailable")
	source := NewTxPendingSource(erroringTxLister{err: wantErr})

	_, err := source.PendingDeposits(context.Background(), uuid.New())
	if !errors.Is(err, wantErr) {
		t.Fatalf("PendingDeposits() error = %v, want %v", err, wantErr)
	}
}

type erroringTxLister struct{ err error }

func (e erroringTxLister) ListUserTransactions(context.Context, transaction.ListFilter) ([]transaction.Transaction, int, error) {
	return nil, 0, e.err
}

// captureHandler is a minimal slog.Handler that records emitted records as
// plain strings for assertion, mirroring the one in
// reconciliation/engine_test.go.
type captureHandler struct {
	records *[]string
}

func (h captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h captureHandler) Handle(_ context.Context, r slog.Record) error {
	var sb strings.Builder
	sb.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		sb.WriteString(fmt.Sprintf(" %s=%v", a.Key, a.Value))
		return true
	})
	*h.records = append(*h.records, sb.String())
	return nil
}
func (h captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h captureHandler) WithGroup(string) slog.Handler      { return h }

type erroringUserPusher struct{ err error }

func (e erroringUserPusher) PushToUser(context.Context, uuid.UUID, string, any) error {
	return e.err
}

func TestWSNotifier_PushValuation_LogsFailurePushError(t *testing.T) {
	pushErr := errors.New("hub unavailable")
	var logs []string
	logger := slog.New(captureHandler{records: &logs})
	notifier := NewWSNotifier(erroringUserPusher{err: pushErr}, logger)

	notifier.PushValuation(uuid.New(), portfolio.Valuation{})

	if len(logs) != 1 {
		t.Fatalf("expected exactly one log record, got %d: %v", len(logs), logs)
	}
	if !strings.Contains(logs[0], pushErr.Error()) {
		t.Fatalf("log entry missing the push error: %q", logs[0])
	}
}

type noopUserPusher struct{}

func (noopUserPusher) PushToUser(context.Context, uuid.UUID, string, any) error { return nil }

func TestWSNotifier_PushValuation_NoLogOnSuccess(t *testing.T) {
	var logs []string
	logger := slog.New(captureHandler{records: &logs})
	notifier := NewWSNotifier(noopUserPusher{}, logger)

	notifier.PushValuation(uuid.New(), portfolio.Valuation{})

	if len(logs) != 0 {
		t.Fatalf("expected no log records on a successful push, got %d: %v", len(logs), logs)
	}
}

func TestNewWSNotifier_NilLoggerDoesNotPanic(t *testing.T) {
	notifier := NewWSNotifier(noopUserPusher{}, nil)
	notifier.PushValuation(uuid.New(), portfolio.Valuation{})
}
