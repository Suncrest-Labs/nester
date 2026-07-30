package reconciliation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
)

type TransactionStatusUpdater interface {
	UpdateStatus(ctx context.Context, hash string, status transaction.TransactionStatus, confirmedAt *time.Time, errorReason string) (transaction.Transaction, error)
}

type CorrectionService struct {
	repo         Repository
	transactions TransactionStatusUpdater
	clock        func() time.Time
}

func NewCorrectionService(repo Repository, transactions TransactionStatusUpdater) *CorrectionService {
	return &CorrectionService{
		repo:         repo,
		transactions: transactions,
		clock:        func() time.Time { return time.Now().UTC() },
	}
}

func (s *CorrectionService) SetClock(clock func() time.Time) {
	s.clock = clock
}

func (s *CorrectionService) ResolveStuckTransaction(ctx context.Context, finding Finding, onChainStatus transaction.TransactionStatus) error {
	if finding.Type != TypeStuck || finding.EntityType != "transaction" {
		return fmt.Errorf("reconciliation: finding is not a stuck transaction")
	}
	if onChainStatus == transaction.StatusPending || onChainStatus == "" {
		return fmt.Errorf("reconciliation: unresolved on-chain status cannot be auto-corrected")
	}

	confirmedAt := s.clock()
	errorReason := ""
	if onChainStatus == transaction.StatusFailed {
		errorReason = "resolved by reconciliation"
	}
	if _, err := s.transactions.UpdateStatus(ctx, finding.EntityID, onChainStatus, &confirmedAt, errorReason); err != nil {
		return err
	}
	return s.repo.RecordCorrection(ctx, finding.ID, strings.TrimSpace("stuck transaction advanced to "+string(onChainStatus)))
}
