package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
	"github.com/suncrestlabs/nester/apps/api/internal/metrics"
)

// fakeTransactionRepo is an in-memory transaction.Repository for poller tests.
type fakeTransactionRepo struct {
	mu  sync.Mutex
	txs map[string]transaction.Transaction // keyed by tx hash
}

func newFakeTransactionRepo(seed ...transaction.Transaction) *fakeTransactionRepo {
	r := &fakeTransactionRepo{txs: make(map[string]transaction.Transaction)}
	for _, tx := range seed {
		r.txs[tx.TxHash] = tx
	}
	return r
}

func (r *fakeTransactionRepo) Upsert(_ context.Context, model transaction.Transaction) (transaction.Transaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.txs[model.TxHash] = model
	return model, nil
}

func (r *fakeTransactionRepo) GetByHash(_ context.Context, hash string) (transaction.Transaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tx, ok := r.txs[hash]
	if !ok {
		return transaction.Transaction{}, transaction.ErrTransactionNotFound
	}
	return tx, nil
}

func (r *fakeTransactionRepo) UpdateStatus(_ context.Context, hash string, status transaction.TransactionStatus, confirmedAt *time.Time, errorReason string) (transaction.Transaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tx, ok := r.txs[hash]
	if !ok {
		return transaction.Transaction{}, transaction.ErrTransactionNotFound
	}
	tx.Status = status
	tx.ConfirmedAt = confirmedAt
	tx.ErrorReason = errorReason
	tx.UpdatedAt = time.Now().UTC()
	r.txs[hash] = tx
	return tx, nil
}

func (r *fakeTransactionRepo) ListPendingOlderThan(_ context.Context, cutoff time.Time) ([]transaction.Transaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []transaction.Transaction
	for _, tx := range r.txs {
		if tx.Status == transaction.StatusPending && tx.TxHash != "" && !tx.CreatedAt.After(cutoff) {
			out = append(out, tx)
		}
	}
	return out, nil
}

func (r *fakeTransactionRepo) ListUserTransactions(_ context.Context, filter transaction.ListFilter) ([]transaction.Transaction, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var matched []transaction.Transaction
	for _, tx := range r.txs {
		if filter.VaultID != uuid.Nil && tx.VaultID != filter.VaultID {
			continue
		}
		if filter.Type != "" && string(tx.Type) != filter.Type {
			continue
		}
		if filter.Status != "" && string(tx.Status) != filter.Status {
			continue
		}
		matched = append(matched, tx)
	}

	total := len(matched)
	if filter.Offset >= total {
		return []transaction.Transaction{}, total, nil
	}
	end := total
	if filter.Limit > 0 && filter.Offset+filter.Limit < end {
		end = filter.Offset + filter.Limit
	}
	out := matched[filter.Offset:end]
	if out == nil {
		out = []transaction.Transaction{}
	}
	return out, total, nil
}

// horizonStub returns a Horizon server that responds to GET /transactions/{hash}
// based on the provided per-hash responses. A hash with no entry returns 404
// (Horizon's "not yet ingested / still pending" signal).
func horizonStub(t *testing.T, successByHash map[string]horizonTransactionResponse) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hash := strings.TrimPrefix(r.URL.Path, "/transactions/")
		resp, ok := successByHash[hash]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"successful":` + boolStr(resp.Successful) +
			`,"created_at":"` + resp.CreatedAt + `","result_xdr":"` + resp.ResultXdr + `"}`))
	}))
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

type recordingNotifier struct {
	mu    sync.Mutex
	calls []transaction.Transaction
}

func (n *recordingNotifier) notify(_ context.Context, tx transaction.Transaction) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls = append(n.calls, tx)
}

func newPendingTx(txType transaction.TransactionType, hash string, age time.Duration) transaction.Transaction {
	now := time.Now().UTC()
	return transaction.Transaction{
		ID:        uuid.New(),
		VaultID:   uuid.New(),
		Type:      txType,
		Amount:    decimal.NewFromInt(100),
		Currency:  "USDC",
		TxHash:    hash,
		Status:    transaction.StatusPending,
		CreatedAt: now.Add(-age),
		UpdatedAt: now.Add(-age),
	}
}

func TestTransactionPoller_Tick_ConfirmsPendingTransaction(t *testing.T) {
	tx := newPendingTx(transaction.TypeDeposit, "abc123", time.Minute)
	repo := newFakeTransactionRepo(tx)

	horizon := horizonStub(t, map[string]horizonTransactionResponse{
		"abc123": {Successful: true, CreatedAt: time.Now().UTC().Format(time.RFC3339)},
	})
	defer horizon.Close()

	svc := NewTransactionService(repo, horizon.URL)
	notifier := &recordingNotifier{}
	poller := NewTransactionPoller(
		TransactionPollerConfig{Enabled: true, Interval: time.Hour, MinAge: 30 * time.Second},
		svc,
		notifier.notify,
		nil,
	)

	poller.Tick(context.Background())

	stored, err := repo.GetByHash(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if stored.Status != transaction.StatusCompleted {
		t.Fatalf("expected status completed, got %q", stored.Status)
	}
	if stored.ConfirmedAt == nil {
		t.Fatal("expected confirmed_at to be set")
	}
	if len(notifier.calls) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(notifier.calls))
	}
	if notifier.calls[0].Status != transaction.StatusCompleted || notifier.calls[0].TxHash != "abc123" {
		t.Errorf("unexpected broadcast payload: %+v", notifier.calls[0])
	}
}

func TestTransactionPoller_Tick_MarksFailedTransaction(t *testing.T) {
	tx := newPendingTx(transaction.TypeWithdrawal, "fail99", time.Minute)
	repo := newFakeTransactionRepo(tx)

	horizon := horizonStub(t, map[string]horizonTransactionResponse{
		"fail99": {Successful: false, CreatedAt: time.Now().UTC().Format(time.RFC3339), ResultXdr: "AAAA"},
	})
	defer horizon.Close()

	svc := NewTransactionService(repo, horizon.URL)
	notifier := &recordingNotifier{}
	poller := NewTransactionPoller(
		TransactionPollerConfig{Enabled: true, Interval: time.Hour, MinAge: 30 * time.Second},
		svc, notifier.notify, nil,
	)

	poller.Tick(context.Background())

	stored, _ := repo.GetByHash(context.Background(), "fail99")
	if stored.Status != transaction.StatusFailed {
		t.Fatalf("expected status failed, got %q", stored.Status)
	}
	if len(notifier.calls) != 1 || notifier.calls[0].Status != transaction.StatusFailed {
		t.Fatalf("expected a single failed broadcast, got %+v", notifier.calls)
	}
}

func TestTransactionPoller_Tick_SkipsTransactionsYoungerThanMinAge(t *testing.T) {
	// 5s old, MinAge 30s — must not even be listed, so Horizon is never hit.
	tx := newPendingTx(transaction.TypeDeposit, "fresh1", 5*time.Second)
	repo := newFakeTransactionRepo(tx)

	horizon := horizonStub(t, map[string]horizonTransactionResponse{
		"fresh1": {Successful: true, CreatedAt: time.Now().UTC().Format(time.RFC3339)},
	})
	defer horizon.Close()

	svc := NewTransactionService(repo, horizon.URL)
	notifier := &recordingNotifier{}
	poller := NewTransactionPoller(
		TransactionPollerConfig{Enabled: true, Interval: time.Hour, MinAge: 30 * time.Second},
		svc, notifier.notify, nil,
	)

	poller.Tick(context.Background())

	stored, _ := repo.GetByHash(context.Background(), "fresh1")
	if stored.Status != transaction.StatusPending {
		t.Errorf("young tx should remain pending, got %q", stored.Status)
	}
	if len(notifier.calls) != 0 {
		t.Errorf("no broadcast expected for young tx, got %d", len(notifier.calls))
	}
}

func TestTransactionPoller_Tick_LeavesStillPendingOnChainUntouched(t *testing.T) {
	// Old enough to poll, but Horizon returns 404 (not yet ingested).
	tx := newPendingTx(transaction.TypeDeposit, "pend42", time.Minute)
	repo := newFakeTransactionRepo(tx)

	horizon := horizonStub(t, nil) // every hash 404s
	defer horizon.Close()

	svc := NewTransactionService(repo, horizon.URL)
	notifier := &recordingNotifier{}
	poller := NewTransactionPoller(
		TransactionPollerConfig{Enabled: true, Interval: time.Hour, MinAge: 30 * time.Second},
		svc, notifier.notify, nil,
	)

	poller.Tick(context.Background())

	stored, _ := repo.GetByHash(context.Background(), "pend42")
	if stored.Status != transaction.StatusPending {
		t.Errorf("expected still pending, got %q", stored.Status)
	}
	if len(notifier.calls) != 0 {
		t.Errorf("no broadcast expected, got %d", len(notifier.calls))
	}
}

func TestTransactionPoller_Run_StopsOnContextCancel(t *testing.T) {
	repo := newFakeTransactionRepo()
	svc := NewTransactionService(repo, "")
	poller := NewTransactionPoller(
		TransactionPollerConfig{Enabled: true, Interval: 10 * time.Millisecond, MinAge: 0},
		svc, nil, nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		poller.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
		// Run returned promptly on cancellation — clean shutdown.
	case <-time.After(time.Second):
		t.Fatal("poller did not stop within 1s of context cancellation")
	}
}

func TestTransactionPoller_Run_NoOpWhenDisabled(t *testing.T) {
	tx := newPendingTx(transaction.TypeDeposit, "off001", time.Minute)
	repo := newFakeTransactionRepo(tx)
	svc := NewTransactionService(repo, "")
	notifier := &recordingNotifier{}
	poller := NewTransactionPoller(
		TransactionPollerConfig{Enabled: false},
		svc, notifier.notify, nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // disabled Run must return immediately regardless
	poller.Run(ctx)

	if len(notifier.calls) != 0 {
		t.Errorf("disabled poller must never broadcast, got %d", len(notifier.calls))
	}
}

// ---------------------------------------------------------------------------
// Reconciliation and pending-submission metrics (nester#1108)
// ---------------------------------------------------------------------------

// recordingPollerMetrics captures what the poller reports, so the tests below
// assert on the operational signal rather than on Prometheus internals.
type recordingPollerMetrics struct {
	runs        []metrics.ReconcileOutcome
	divergences []metrics.DivergenceKind
	pendingLast struct {
		count  int
		oldest time.Duration
		set    bool
	}
}

func (r *recordingPollerMetrics) RecordReconcileRun(outcome metrics.ReconcileOutcome) {
	r.runs = append(r.runs, outcome)
}

func (r *recordingPollerMetrics) RecordReconcileDivergence(kind metrics.DivergenceKind) {
	r.divergences = append(r.divergences, kind)
}

func (r *recordingPollerMetrics) SetPendingSubmissions(count int, oldest time.Duration) {
	r.pendingLast.count = count
	r.pendingLast.oldest = oldest
	r.pendingLast.set = true
}

// A transaction old enough to poll that the chain still has not resolved is a
// stuck submission — money in flight the user cannot see. Before #1108 this
// was logged at most; it must now reach the metrics an alert reads.
func TestTransactionPoller_Tick_RecordsStuckDivergence(t *testing.T) {
	tx := newPendingTx(transaction.TypeDeposit, "stuck1", time.Minute)
	repo := newFakeTransactionRepo(tx)

	horizon := horizonStub(t, nil) // 404: not yet resolved on chain
	defer horizon.Close()

	svc := NewTransactionService(repo, horizon.URL)
	rec := &recordingPollerMetrics{}
	poller := NewTransactionPoller(
		TransactionPollerConfig{Enabled: true, Interval: time.Hour, MinAge: 30 * time.Second},
		svc, nil, nil,
	)
	poller.SetMetrics(rec)

	poller.Tick(context.Background())

	if len(rec.divergences) != 1 || rec.divergences[0] != metrics.DivergenceStuck {
		t.Fatalf("divergences = %v, want exactly one stuck", rec.divergences)
	}
	if len(rec.runs) != 1 || rec.runs[0] != metrics.ReconcileCompleted {
		t.Fatalf("runs = %v, want one completed", rec.runs)
	}
}

// A transaction that reconciles to a terminal state is not a divergence: the
// system worked. Counting it would make the divergence signal fire on healthy
// traffic and train operators to ignore it.
func TestTransactionPoller_Tick_ResolvedTransactionIsNotADivergence(t *testing.T) {
	tx := newPendingTx(transaction.TypeDeposit, "good77", time.Minute)
	repo := newFakeTransactionRepo(tx)

	horizon := horizonStub(t, map[string]horizonTransactionResponse{
		"good77": {Successful: true, CreatedAt: time.Now().UTC().Format(time.RFC3339)},
	})
	defer horizon.Close()

	svc := NewTransactionService(repo, horizon.URL)
	rec := &recordingPollerMetrics{}
	poller := NewTransactionPoller(
		TransactionPollerConfig{Enabled: true, Interval: time.Hour, MinAge: 30 * time.Second},
		svc, nil, nil,
	)
	poller.SetMetrics(rec)

	poller.Tick(context.Background())

	if len(rec.divergences) != 0 {
		t.Fatalf("divergences = %v, want none for a transaction that resolved", rec.divergences)
	}
	if len(rec.runs) != 1 || rec.runs[0] != metrics.ReconcileCompleted {
		t.Fatalf("runs = %v, want one completed", rec.runs)
	}
}

// The backlog gauges are published every pass, including when the queue is
// empty — otherwise a drained queue would leave the previous non-zero reading
// in place and the alert would never clear.
func TestTransactionPoller_Tick_PublishesPendingBacklog(t *testing.T) {
	older := newPendingTx(transaction.TypeDeposit, "old01", 10*time.Minute)
	newer := newPendingTx(transaction.TypeWithdrawal, "new01", time.Minute)
	repo := newFakeTransactionRepo(older, newer)

	horizon := horizonStub(t, nil)
	defer horizon.Close()

	svc := NewTransactionService(repo, horizon.URL)
	rec := &recordingPollerMetrics{}
	poller := NewTransactionPoller(
		TransactionPollerConfig{Enabled: true, Interval: time.Hour, MinAge: 30 * time.Second},
		svc, nil, nil,
	)
	poller.SetMetrics(rec)

	poller.Tick(context.Background())

	if !rec.pendingLast.set {
		t.Fatal("pending backlog was never published")
	}
	if rec.pendingLast.count != 2 {
		t.Fatalf("pending count = %d, want 2", rec.pendingLast.count)
	}
	// The oldest entry drives the age, not the most recent one.
	if rec.pendingLast.oldest < 9*time.Minute {
		t.Fatalf("oldest age = %s, want at least 9m (the older of the two)", rec.pendingLast.oldest)
	}
}

// A poller with no recorder wired must behave exactly as before. Losing
// observability must never be able to break reconciliation itself.
func TestTransactionPoller_Tick_WorksWithoutMetrics(t *testing.T) {
	tx := newPendingTx(transaction.TypeDeposit, "nom001", time.Minute)
	repo := newFakeTransactionRepo(tx)

	horizon := horizonStub(t, map[string]horizonTransactionResponse{
		"nom001": {Successful: true, CreatedAt: time.Now().UTC().Format(time.RFC3339)},
	})
	defer horizon.Close()

	svc := NewTransactionService(repo, horizon.URL)
	poller := NewTransactionPoller(
		TransactionPollerConfig{Enabled: true, Interval: time.Hour, MinAge: 30 * time.Second},
		svc, nil, nil,
	)
	poller.SetMetrics(nil) // explicitly nil: must not panic

	poller.Tick(context.Background())

	stored, _ := repo.GetByHash(context.Background(), "nom001")
	if stored.Status != transaction.StatusCompleted {
		t.Fatalf("status = %q, want completed", stored.Status)
	}
}
