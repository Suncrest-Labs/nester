package scheduler

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/ledger"
)

type mockReconciliationVaultLister struct {
	vaults []ReconcileVaultInfo
}

func (m *mockReconciliationVaultLister) ListActiveForReconciliation(ctx context.Context) ([]ReconcileVaultInfo, error) {
	return m.vaults, nil
}

type mockLedgerRepo struct {
	ledger.Repository
	poolBalance int64
	records     []ledger.ReconciliationRecord
}

func (m *mockLedgerRepo) GetVaultPoolBalance(ctx context.Context, vaultID uuid.UUID) (int64, error) {
	return m.poolBalance, nil
}

func (m *mockLedgerRepo) SumUserPositionBalances(ctx context.Context, vaultID uuid.UUID) (int64, error) {
	return 0, nil
}

func (m *mockLedgerRepo) CreateReconciliationRecord(ctx context.Context, rec ledger.ReconciliationRecord) error {
	m.records = append(m.records, rec)
	return nil
}

type mockChainReader struct {
	ledger.ChainReader
	onChainBalance   int64
	totalSharesPrice int64
}

func (m *mockChainReader) ReadVaultBalance(ctx context.Context, contractAddress string) (int64, error) {
	return m.onChainBalance, nil
}

func (m *mockChainReader) ReadTotalSharesTimesPrice(ctx context.Context, contractAddress string) (int64, error) {
	return m.totalSharesPrice, nil
}

// runReconciliationTick wires up a job with the given mainnet flag and threshold,
// runs a single tick with a fixed drift, and returns the written record plus the
// captured log output so callers can assert on the escalation branch taken.
func runReconciliationTick(t *testing.T, isMainnet bool, mainnetDollarThreshold float64) (ledger.ReconciliationRecord, string) {
	t.Helper()

	vaultID := uuid.New()
	vaultsLister := &mockReconciliationVaultLister{
		vaults: []ReconcileVaultInfo{
			{ID: vaultID, ContractAddress: "CVAULTMAINNET", Currency: "USDC"},
		},
	}

	ledgerRepo := &mockLedgerRepo{
		poolBalance: 20_000_000, // 2 USDC
	}

	chainReader := &mockChainReader{
		onChainBalance: 10_000_000, // 1 USDC (diff is 1 USDC = 10,000,000 stroops)
	}

	cfg := ledger.ReconciliationConfig{
		Enabled:                true,
		Interval:               time.Minute,
		ToleranceStroops:       1_000, // small tolerance
		MainnetDollarThreshold: mainnetDollarThreshold,
		IsMainnet:              isMainnet,
	}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	job := NewLedgerReconciliationJob(LedgerReconciliationDeps{
		LedgerRepo:  ledgerRepo,
		VaultLister: vaultsLister,
		ChainReader: chainReader,
		Logger:      logger,
		Config:      cfg,
	})

	job.Tick(context.Background())

	if len(ledgerRepo.records) != 1 {
		t.Fatalf("expected 1 reconciliation record, got %d", len(ledgerRepo.records))
	}
	return ledgerRepo.records[0], logBuf.String()
}

func TestLedgerReconciliationJobMainnetDollarThresholdPaging(t *testing.T) {
	rec, logOutput := runReconciliationTick(t, true, 0.5) // threshold 0.5 USDC = 5,000,000 stroops

	if rec.Status != "drift" {
		t.Fatalf("expected status drift, got %s", rec.Status)
	}
	if !strings.Contains(logOutput, "PAGER ALERT") {
		t.Fatalf("expected escalation branch to log a PAGER ALERT, got log output: %s", logOutput)
	}
	for _, field := range []string{"vault_id=", "difference=", "dollar_threshold="} {
		if !strings.Contains(logOutput, field) {
			t.Errorf("expected PAGER ALERT log to include structured field %q, got: %s", field, logOutput)
		}
	}
}

func TestLedgerReconciliationJobDriftWithoutEscalation(t *testing.T) {
	// Non-mainnet drift over the dollar threshold must NOT page: only the plain
	// drift alert should fire.
	rec, logOutput := runReconciliationTick(t, false, 0.5)

	if rec.Status != "drift" {
		t.Fatalf("expected status drift, got %s", rec.Status)
	}
	if strings.Contains(logOutput, "PAGER ALERT") {
		t.Fatalf("did not expect PAGER ALERT for non-mainnet drift, got log output: %s", logOutput)
	}
	if !strings.Contains(logOutput, "drift beyond tolerance") {
		t.Fatalf("expected plain drift alert to log, got: %s", logOutput)
	}

	// Mainnet drift under the dollar threshold must also NOT page.
	recUnder, logUnder := runReconciliationTick(t, true, 50) // threshold far above the 1 USDC drift
	if recUnder.Status != "drift" {
		t.Fatalf("expected status drift, got %s", recUnder.Status)
	}
	if strings.Contains(logUnder, "PAGER ALERT") {
		t.Fatalf("did not expect PAGER ALERT for drift under mainnet threshold, got log output: %s", logUnder)
	}
}

func TestIsMainnetEnvironment(t *testing.T) {
	_ = os.Setenv("STELLAR_NETWORK", "mainnet")
	defer os.Unsetenv("STELLAR_NETWORK")

	if !isMainnetEnvironment() {
		t.Fatal("expected isMainnetEnvironment to report true when STELLAR_NETWORK=mainnet")
	}
}
