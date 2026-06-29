package scheduler

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/performance"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

// VaultAPYListerFunc implements APYVaultLister via a closure. Used in main.go
// to adapt postgres.VaultRepository.ListActiveVaultsForAPYCheck without an import cycle.
type VaultAPYListerFunc func(ctx context.Context) ([]APYVaultInfo, error)

func (f VaultAPYListerFunc) ListActiveVaultsForAPYCheck(ctx context.Context) ([]APYVaultInfo, error) {
	return f(ctx)
}

// VaultAPYUpdaterFunc implements APYAlertUpdater via a closure.
type VaultAPYUpdaterFunc func(ctx context.Context, vaultID uuid.UUID, sentAt time.Time) error

func (f VaultAPYUpdaterFunc) UpdateAPYAlertSentAt(ctx context.Context, vaultID uuid.UUID, sentAt time.Time) error {
	return f(ctx, vaultID, sentAt)
}

// APYSnapshotReaderFunc implements APYSnapshotReader via a closure.
type APYSnapshotReaderFunc func(ctx context.Context, vaultID uuid.UUID, since time.Time) ([]performance.Snapshot, error)

func (f APYSnapshotReaderFunc) HistoryForVault(ctx context.Context, vaultID uuid.UUID, since time.Time) ([]performance.Snapshot, error) {
	return f(ctx, vaultID, since)
}

// APYNotifierAdapter adapts notifications.Dispatcher to APYNotifier.
type APYNotifierAdapter struct {
	D *notifications.Dispatcher
}

func (a APYNotifierAdapter) Send(ctx context.Context, userID uuid.UUID, evt notifications.EventType, title, body string, payload map[string]any) error {
	return a.D.Send(ctx, userID, evt, title, body, payload)
}
