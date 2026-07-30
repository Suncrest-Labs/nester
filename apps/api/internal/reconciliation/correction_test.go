package reconciliation

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
)

type fakeTransactionUpdater struct {
	hash   string
	status transaction.TransactionStatus
}

func (u *fakeTransactionUpdater) UpdateStatus(ctx context.Context, hash string, status transaction.TransactionStatus, confirmedAt *time.Time, errorReason string) (transaction.Transaction, error) {
	u.hash = hash
	u.status = status
	return transaction.Transaction{TxHash: hash, Status: status, ConfirmedAt: confirmedAt}, nil
}

func TestCorrectionServiceOnlyLogsProvablyResolvedStuckTransaction(t *testing.T) {
	repo := &fakeRepo{}
	transactions := &fakeTransactionUpdater{}
	service := NewCorrectionService(repo, transactions)
	finding := Finding{
		ID:         uuid.New(),
		Type:       TypeStuck,
		EntityType: "transaction",
		EntityID:   "tx-hash",
	}

	if err := service.ResolveStuckTransaction(context.Background(), finding, transaction.StatusCompleted); err != nil {
		t.Fatalf("ResolveStuckTransaction() error = %v", err)
	}
	if transactions.hash != "tx-hash" || transactions.status != transaction.StatusCompleted {
		t.Fatalf("transaction update = %s/%s", transactions.hash, transactions.status)
	}
	if repo.corrections != 1 {
		t.Fatalf("corrections = %d, want 1", repo.corrections)
	}
}

func TestCorrectionServiceRejectsPendingStatus(t *testing.T) {
	service := NewCorrectionService(&fakeRepo{}, &fakeTransactionUpdater{})
	err := service.ResolveStuckTransaction(context.Background(), Finding{
		ID:         uuid.New(),
		Type:       TypeStuck,
		EntityType: "transaction",
		EntityID:   "tx-hash",
	}, transaction.StatusPending)
	if err == nil {
		t.Fatal("expected pending status to be rejected")
	}
}
