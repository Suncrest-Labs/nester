package postgres

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestAdminRepositoryGetVaultHealthDashboard(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewAdminRepository(db)
	ctx := context.Background()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT
			COALESCE((SELECT SUM(current_balance) FROM vaults), 0)::text,
			COALESCE((
				SELECT COUNT(*) FROM (
					SELECT DISTINCT COALESCE(vt.user_id, v.user_id) AS depositor_id
					FROM vault_transactions vt
					JOIN vaults v ON v.id = vt.vault_id
					WHERE vt.type = 'deposit'
				) d
			), 0)
	`)).WillReturnRows(sqlmock.NewRows([]string{"total_tvl", "total_depositors"}).
		AddRow("5234891.00", int64(892)))

	vaultID := uuid.New()
	rebalanceAt := time.Date(2026, 5, 26, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery("FROM vaults v").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "current_balance", "status",
			"depositor_count", "pending_count", "last_rebalance_at",
			"realized_apy", "realized_apy",
		}).AddRow(
			vaultID.String(),
			"Conservative",
			"1234000.00",
			"active",
			int64(234),
			int64(3),
			rebalanceAt,
			"8.2400",
			"10.0000",
		))

	result, err := repo.GetVaultHealthDashboard(ctx)
	if err != nil {
		t.Fatalf("GetVaultHealthDashboard() error = %v", err)
	}
	if !result.TotalTVL.Equal(decimal.RequireFromString("5234891.00")) {
		t.Fatalf("total_tvl = %s, want 5234891.00", result.TotalTVL)
	}
	if result.TotalDepositors != 892 {
		t.Fatalf("total_depositors = %d, want 892", result.TotalDepositors)
	}
	if len(result.Vaults) != 1 {
		t.Fatalf("vault count = %d, want 1", len(result.Vaults))
	}
	if result.Vaults[0].PendingTransactions != 3 {
		t.Fatalf("pending_transactions = %d, want 3", result.Vaults[0].PendingTransactions)
	}
	if result.Vaults[0].APY7d == nil || !result.Vaults[0].APY7d.Equal(decimal.RequireFromString("8.24")) {
		t.Fatalf("apy_7d = %+v, want 8.24", result.Vaults[0].APY7d)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
