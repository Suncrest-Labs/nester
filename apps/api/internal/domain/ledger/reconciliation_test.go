package ledger

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
)

type mockChainReader struct {
	balances map[string]int64
}

func (m *mockChainReader) ReadVaultBalance(_ context.Context, contract string) (int64, error) {
	return m.balances[contract], nil
}

func (m *mockChainReader) ReadTotalSharesTimesPrice(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

type recordingRepo struct {
	Repository
	pollBalances map[uuid.UUID]int64
	records      []ReconciliationRecord
}

func (r *recordingRepo) GetVaultPoolBalance(_ context.Context, vaultID uuid.UUID) (int64, error) {
	return r.pollBalances[vaultID], nil
}

func (r *recordingRepo) CreateReconciliationRecord(_ context.Context, rec ReconciliationRecord) error {
	r.records = append(r.records, rec)
	return nil
}

type recordingMetrics struct {
	runs   int
	drifts int
}

func (m *recordingMetrics) IncReconciliationRun(string) {
	m.runs++
}
func (m *recordingMetrics) IncReconciliationDrift() {
	m.drifts++
}
func (m *recordingMetrics) ObserveDriftAmount(string, int64) {}

func TestReconciler_DetectsDrift(t *testing.T) {
	vaultID := uuid.New()
	contract := "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC"

	repo := &recordingRepo{
		pollBalances: map[uuid.UUID]int64{vaultID: 1000},
	}
	chain := &mockChainReader{
		balances: map[string]int64{contract: 1500},
	}
	metrics := &recordingMetrics{}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg := ReconciliationConfig{
		Enabled:          true,
		ToleranceStroops: 100,
	}

	reconciler := NewReconciler(repo, chain, cfg, logger, metrics)
	err := reconciler.ReconcileVaults(context.Background(), []uuid.UUID{vaultID}, map[uuid.UUID]string{vaultID: contract})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if metrics.drifts != 1 {
		t.Fatalf("expected 1 drift metric, got %d", metrics.drifts)
	}
	if len(repo.records) != 1 || repo.records[0].Status != "drift" {
		t.Fatalf("expected drift reconciliation record, got %+v", repo.records)
	}
}
