package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/moneypath"
)

func newVaultSwitchMock(t *testing.T) (*VaultMoneyPathSwitchRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewVaultMoneyPathSwitchRepository(db), mock
}

// A vault with no stored switch must read as released, not as an error: rows
// are created on first use, so absence is the normal state for every vault
// nobody has paused.
func TestVaultMoneyPathSwitchRepository_GetMissingRowIsReleased(t *testing.T) {
	repo, mock := newVaultSwitchMock(t)
	vaultID := uuid.New()

	mock.ExpectQuery(`SELECT vault_id, operation, paused, reason, changed_by, updated_at FROM vault_money_path_switches WHERE vault_id = \$1 AND operation = \$2`).
		WithArgs(vaultID, "deposit").
		WillReturnError(sql.ErrNoRows)

	got, err := repo.GetVaultSwitch(context.Background(), vaultID, moneypath.OperationDeposit)
	if err != nil {
		t.Fatalf("missing row must not be an error: %v", err)
	}
	if got.Paused {
		t.Fatal("missing row must report released, got paused")
	}
	if got.VaultID != vaultID || got.Operation != moneypath.OperationDeposit {
		t.Fatalf("identity not preserved: %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestVaultMoneyPathSwitchRepository_GetFound(t *testing.T) {
	repo, mock := newVaultSwitchMock(t)
	vaultID := uuid.New()
	actor := uuid.New()
	now := time.Now().UTC()

	mock.ExpectQuery(`SELECT vault_id, operation, paused, reason, changed_by, updated_at FROM vault_money_path_switches WHERE vault_id = \$1 AND operation = \$2`).
		WithArgs(vaultID, "withdrawal").
		WillReturnRows(sqlmock.NewRows([]string{"vault_id", "operation", "paused", "reason", "changed_by", "updated_at"}).
			AddRow(vaultID, "withdrawal", true, "oracle stale", actor, now))

	got, err := repo.GetVaultSwitch(context.Background(), vaultID, moneypath.OperationWithdrawal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Paused || got.Reason != "oracle stale" {
		t.Fatalf("unexpected switch: %+v", got)
	}
	if got.ChangedBy == nil || *got.ChangedBy != actor {
		t.Fatalf("changed_by not scanned: %+v", got.ChangedBy)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestVaultMoneyPathSwitchRepository_List(t *testing.T) {
	repo, mock := newVaultSwitchMock(t)
	vaultID := uuid.New()
	now := time.Now().UTC()

	mock.ExpectQuery(`SELECT vault_id, operation, paused, reason, changed_by, updated_at FROM vault_money_path_switches WHERE vault_id = \$1 ORDER BY operation`).
		WithArgs(vaultID).
		WillReturnRows(sqlmock.NewRows([]string{"vault_id", "operation", "paused", "reason", "changed_by", "updated_at"}).
			AddRow(vaultID, "deposit", true, "draining", nil, now).
			AddRow(vaultID, "withdrawal", false, "", nil, now))

	got, err := repo.ListVaultSwitches(context.Background(), vaultID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	if !got[0].Paused || got[1].Paused {
		t.Fatalf("unexpected paused flags: %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// The first pause of a vault must insert; later ones update. Either way the
// stored row is returned so the caller can echo the committed state.
func TestVaultMoneyPathSwitchRepository_SetUpserts(t *testing.T) {
	repo, mock := newVaultSwitchMock(t)
	vaultID := uuid.New()
	actor := uuid.New()
	now := time.Now().UTC()

	mock.ExpectQuery(`INSERT INTO vault_money_path_switches`).
		WithArgs(vaultID, "deposit", true, "draining", uuid.NullUUID{UUID: actor, Valid: true}).
		WillReturnRows(sqlmock.NewRows([]string{"vault_id", "operation", "paused", "reason", "changed_by", "updated_at"}).
			AddRow(vaultID, "deposit", true, "draining", actor, now))

	got, err := repo.SetVaultSwitch(context.Background(), vaultID, moneypath.OperationDeposit, true, "draining", &actor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Paused || got.Reason != "draining" {
		t.Fatalf("unexpected switch: %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
