package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	admindomain "github.com/suncrestlabs/nester/apps/api/internal/domain/admin"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/offramp"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

type AdminRepository struct {
	db *sql.DB
}

func NewAdminRepository(db *sql.DB) *AdminRepository {
	return &AdminRepository{db: db}
}

func (r *AdminRepository) DatabaseHealth(ctx context.Context) (int64, error) {
	start := time.Now()
	var one int
	if err := r.db.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}

func (r *AdminRepository) GetLastEventIndexedAt(ctx context.Context) (*time.Time, error) {
	var last sql.NullTime
	if err := r.db.QueryRowContext(ctx, `SELECT MAX(created_at) FROM settlements`).Scan(&last); err != nil {
		return nil, err
	}
	if !last.Valid {
		return nil, nil
	}
	t := last.Time.UTC()
	return &t, nil
}

func (r *AdminRepository) GetDashboardMetrics(ctx context.Context) (admindomain.DashboardMetrics, error) {
	const query = `
		SELECT
			COALESCE((SELECT SUM(current_balance) FROM vaults), 0)::text AS total_tvl,
			(SELECT COUNT(*) FROM users) AS total_users,
			(SELECT COUNT(*) FROM vaults WHERE status = 'active') AS active_vaults,
			COALESCE((SELECT SUM(GREATEST(current_balance - total_deposited, 0)) FROM vaults), 0)::text AS total_yield_distributed,
			(SELECT COUNT(*) FROM settlements) AS settlements_total,
			(SELECT COUNT(*) FROM settlements WHERE status IN ('initiated', 'liquidity_matched', 'fiat_dispatched')) AS settlements_pending,
			(SELECT COUNT(*) FROM settlements WHERE status = 'confirmed' AND completed_at >= NOW() - INTERVAL '24 hours') AS settlements_completed_24h,
			(SELECT COUNT(*) FROM settlements WHERE status = 'failed' AND completed_at >= NOW() - INTERVAL '24 hours') AS settlements_failed_24h,
			COALESCE((SELECT SUM(amount) FROM settlements WHERE created_at >= NOW() - INTERVAL '24 hours'), 0)::text AS settlements_volume_24h
	`

	var (
		totalTVL              string
		totalUsers            int64
		activeVaults          int64
		totalYieldDistributed string
		settlementsTotal      int64
		settlementsPending    int64
		settlementsCompleted  int64
		settlementsFailed     int64
		settlementsVolume24h  string
	)

	if err := r.db.QueryRowContext(ctx, query).Scan(
		&totalTVL,
		&totalUsers,
		&activeVaults,
		&totalYieldDistributed,
		&settlementsTotal,
		&settlementsPending,
		&settlementsCompleted,
		&settlementsFailed,
		&settlementsVolume24h,
	); err != nil {
		return admindomain.DashboardMetrics{}, err
	}

	parsedTVL, err := decimal.NewFromString(totalTVL)
	if err != nil {
		return admindomain.DashboardMetrics{}, fmt.Errorf("parse total_tvl: %w", err)
	}
	parsedYield, err := decimal.NewFromString(totalYieldDistributed)
	if err != nil {
		return admindomain.DashboardMetrics{}, fmt.Errorf("parse total_yield_distributed: %w", err)
	}
	parsedVolume, err := decimal.NewFromString(settlementsVolume24h)
	if err != nil {
		return admindomain.DashboardMetrics{}, fmt.Errorf("parse settlements volume_24h: %w", err)
	}

	return admindomain.DashboardMetrics{
		TotalTVL:              parsedTVL,
		TotalUsers:            totalUsers,
		ActiveVaults:          activeVaults,
		TotalYieldDistributed: parsedYield,
		Settlements: admindomain.DashboardSettlementMetrics{
			Total:        settlementsTotal,
			Pending:      settlementsPending,
			Completed24h: settlementsCompleted,
			Failed24h:    settlementsFailed,
			Volume24h:    parsedVolume,
		},
	}, nil
}

func (r *AdminRepository) ListVaults(
	ctx context.Context,
	filter admindomain.VaultListFilter,
) ([]admindomain.VaultSummary, int, error) {
	where, args := buildVaultWhere(filter)

	countQuery := `SELECT COUNT(*) FROM vaults v JOIN users u ON u.id = v.user_id WHERE ` + where
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sortColumn := sanitizeVaultSort(filter.Sort)
	order := sanitizeOrder(filter.Order)
	offset := (filter.Page - 1) * filter.PerPage

	listQuery := fmt.Sprintf(`
		SELECT v.id, v.user_id, u.wallet_address, v.contract_address,
		       v.total_deposited::text, v.current_balance::text, v.currency, v.status,
		       v.created_at, v.updated_at
		FROM vaults v
		JOIN users u ON u.id = v.user_id
		WHERE %s
		ORDER BY %s %s
		LIMIT $%d OFFSET $%d
	`, where, sortColumn, order, len(args)+1, len(args)+2)

	args = append(args, filter.PerPage, offset)
	rows, err := r.db.QueryContext(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]admindomain.VaultSummary, 0)
	for rows.Next() {
		var (
			id              string
			userID          string
			walletAddress   string
			contractAddress string
			totalDeposited  string
			currentBalance  string
			currency        string
			status          string
			createdAt       time.Time
			updatedAt       time.Time
		)

		if err := rows.Scan(
			&id,
			&userID,
			&walletAddress,
			&contractAddress,
			&totalDeposited,
			&currentBalance,
			&currency,
			&status,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, 0, err
		}

		parsedID, err := uuid.Parse(id)
		if err != nil {
			return nil, 0, err
		}
		parsedUserID, err := uuid.Parse(userID)
		if err != nil {
			return nil, 0, err
		}
		parsedDeposited, err := decimal.NewFromString(totalDeposited)
		if err != nil {
			return nil, 0, err
		}
		parsedBalance, err := decimal.NewFromString(currentBalance)
		if err != nil {
			return nil, 0, err
		}

		out = append(out, admindomain.VaultSummary{
			ID:              parsedID,
			UserID:          parsedUserID,
			WalletAddress:   walletAddress,
			ContractAddress: contractAddress,
			TotalDeposited:  parsedDeposited,
			CurrentBalance:  parsedBalance,
			Currency:        currency,
			Status:          vault.VaultStatus(status),
			CreatedAt:       createdAt.UTC(),
			UpdatedAt:       updatedAt.UTC(),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return out, total, nil
}

func (r *AdminRepository) GetVaultDetail(ctx context.Context, id uuid.UUID) (admindomain.VaultDetail, error) {
	const query = `
		SELECT v.id, v.user_id, u.wallet_address, v.contract_address,
		       v.total_deposited::text, v.current_balance::text, v.currency, v.status,
		       v.created_at, v.updated_at
		FROM vaults v
		JOIN users u ON u.id = v.user_id
		WHERE v.id = $1
	`

	var (
		vaultID         string
		userID          string
		walletAddress   string
		contractAddress string
		totalDeposited  string
		currentBalance  string
		currency        string
		status          string
		createdAt       time.Time
		updatedAt       time.Time
	)

	if err := r.db.QueryRowContext(ctx, query, id.String()).Scan(
		&vaultID,
		&userID,
		&walletAddress,
		&contractAddress,
		&totalDeposited,
		&currentBalance,
		&currency,
		&status,
		&createdAt,
		&updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return admindomain.VaultDetail{}, vault.ErrVaultNotFound
		}
		return admindomain.VaultDetail{}, err
	}

	parsedVaultID, err := uuid.Parse(vaultID)
	if err != nil {
		return admindomain.VaultDetail{}, err
	}
	parsedUserID, err := uuid.Parse(userID)
	if err != nil {
		return admindomain.VaultDetail{}, err
	}
	parsedDeposited, err := decimal.NewFromString(totalDeposited)
	if err != nil {
		return admindomain.VaultDetail{}, err
	}
	parsedBalance, err := decimal.NewFromString(currentBalance)
	if err != nil {
		return admindomain.VaultDetail{}, err
	}

	allocations, err := loadAllocations(ctx, r.db, parsedVaultID)
	if err != nil {
		return admindomain.VaultDetail{}, err
	}

	return admindomain.VaultDetail{
		VaultSummary: admindomain.VaultSummary{
			ID:              parsedVaultID,
			UserID:          parsedUserID,
			WalletAddress:   walletAddress,
			ContractAddress: contractAddress,
			TotalDeposited:  parsedDeposited,
			CurrentBalance:  parsedBalance,
			Currency:        currency,
			Status:          vault.VaultStatus(status),
			CreatedAt:       createdAt.UTC(),
			UpdatedAt:       updatedAt.UTC(),
		},
		Allocations: allocations,
	}, nil
}

func (r *AdminRepository) UpdateVaultStatus(
	ctx context.Context,
	id uuid.UUID,
	status vault.VaultStatus,
) (admindomain.VaultDetail, error) {
	result, err := r.db.ExecContext(
		ctx,
		`UPDATE vaults SET status = $2, updated_at = NOW() WHERE id = $1`,
		id.String(),
		string(status),
	)
	if err != nil {
		return admindomain.VaultDetail{}, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return admindomain.VaultDetail{}, err
	}
	if rowsAffected == 0 {
		return admindomain.VaultDetail{}, vault.ErrVaultNotFound
	}

	return r.GetVaultDetail(ctx, id)
}

func (r *AdminRepository) ListSettlements(
	ctx context.Context,
	filter admindomain.SettlementListFilter,
) ([]admindomain.SettlementSummary, int, error) {
	where, args := buildSettlementWhere(filter)

	countQuery := `SELECT COUNT(*) FROM settlements s JOIN users u ON u.id = s.user_id WHERE ` + where
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sortColumn := sanitizeSettlementSort(filter.Sort)
	order := sanitizeOrder(filter.Order)
	offset := (filter.Page - 1) * filter.PerPage

	listQuery := fmt.Sprintf(`
		SELECT s.id, s.user_id, s.vault_id,
		       s.amount::text, s.currency, s.fiat_currency, s.fiat_amount::text, s.exchange_rate::text,
		       s.destination_type, s.destination_provider, s.destination_account_number, s.destination_account_name, s.destination_bank_code,
		       s.status, s.created_at, s.completed_at, u.wallet_address
		FROM settlements s
		JOIN users u ON u.id = s.user_id
		WHERE %s
		ORDER BY %s %s
		LIMIT $%d OFFSET $%d
	`, where, sortColumn, order, len(args)+1, len(args)+2)

	args = append(args, filter.PerPage, offset)
	rows, err := r.db.QueryContext(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]admindomain.SettlementSummary, 0)
	for rows.Next() {
		var (
			id            string
			userID        string
			vaultID       string
			amount        string
			currency      string
			fiatCurrency  string
			fiatAmount    string
			exchangeRate  string
			destType      string
			destProvider  string
			destAccountNo string
			destName      string
			destBankCode  string
			status        string
			createdAt     time.Time
			completedAt   sql.NullTime
			walletAddress string
		)

		if err := rows.Scan(
			&id,
			&userID,
			&vaultID,
			&amount,
			&currency,
			&fiatCurrency,
			&fiatAmount,
			&exchangeRate,
			&destType,
			&destProvider,
			&destAccountNo,
			&destName,
			&destBankCode,
			&status,
			&createdAt,
			&completedAt,
			&walletAddress,
		); err != nil {
			return nil, 0, err
		}

		parsedID, err := uuid.Parse(id)
		if err != nil {
			return nil, 0, err
		}
		parsedUserID, err := uuid.Parse(userID)
		if err != nil {
			return nil, 0, err
		}
		parsedVaultID, err := uuid.Parse(vaultID)
		if err != nil {
			return nil, 0, err
		}
		parsedAmount, err := decimal.NewFromString(amount)
		if err != nil {
			return nil, 0, err
		}
		parsedFiatAmount, err := decimal.NewFromString(fiatAmount)
		if err != nil {
			return nil, 0, err
		}
		parsedExchangeRate, err := decimal.NewFromString(exchangeRate)
		if err != nil {
			return nil, 0, err
		}

		var completedAtPtr *time.Time
		if completedAt.Valid {
			t := completedAt.Time.UTC()
			completedAtPtr = &t
		}

		out = append(out, admindomain.SettlementSummary{
			Settlement: offramp.Settlement{
				ID:           parsedID,
				UserID:       parsedUserID,
				VaultID:      parsedVaultID,
				Amount:       parsedAmount,
				Currency:     currency,
				FiatCurrency: fiatCurrency,
				FiatAmount:   parsedFiatAmount,
				ExchangeRate: parsedExchangeRate,
				Destination: offramp.Destination{
					Type:          destType,
					Provider:      destProvider,
					AccountNumber: destAccountNo,
					AccountName:   destName,
					BankCode:      destBankCode,
				},
				Status:      offramp.SettlementStatus(status),
				CreatedAt:   createdAt.UTC(),
				CompletedAt: completedAtPtr,
			},
			WalletAddress: walletAddress,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return out, total, nil
}

func (r *AdminRepository) ListUsers(
	ctx context.Context,
	filter admindomain.UserListFilter,
) ([]admindomain.UserSummary, int, error) {
	where, args := buildUserWhere(filter)

	countQuery := `SELECT COUNT(*) FROM users u WHERE ` + where
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sortColumn := sanitizeUserSort(filter.Sort)
	order := sanitizeOrder(filter.Order)
	offset := (filter.Page - 1) * filter.PerPage

	listQuery := fmt.Sprintf(`
		SELECT u.id, u.wallet_address, u.display_name, u.kyc_status, u.created_at, u.updated_at,
		       COUNT(v.id) AS vault_count,
		       COALESCE(SUM(v.total_deposited), 0)::text AS total_deposited
		FROM users u
		LEFT JOIN vaults v ON v.user_id = u.id
		WHERE %s
		GROUP BY u.id, u.wallet_address, u.display_name, u.kyc_status, u.created_at, u.updated_at
		ORDER BY %s %s
		LIMIT $%d OFFSET $%d
	`, where, sortColumn, order, len(args)+1, len(args)+2)

	args = append(args, filter.PerPage, offset)
	rows, err := r.db.QueryContext(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]admindomain.UserSummary, 0)
	for rows.Next() {
		var (
			id             string
			walletAddress  string
			displayName    string
			kycStatus      string
			createdAt      time.Time
			updatedAt      time.Time
			vaultCount     int64
			totalDeposited string
		)
		if err := rows.Scan(
			&id,
			&walletAddress,
			&displayName,
			&kycStatus,
			&createdAt,
			&updatedAt,
			&vaultCount,
			&totalDeposited,
		); err != nil {
			return nil, 0, err
		}

		parsedID, err := uuid.Parse(id)
		if err != nil {
			return nil, 0, err
		}
		parsedTotalDeposited, err := decimal.NewFromString(totalDeposited)
		if err != nil {
			return nil, 0, err
		}

		out = append(out, admindomain.UserSummary{
			ID:             parsedID,
			WalletAddress:  walletAddress,
			DisplayName:    displayName,
			KYCStatus:      user.KYCStatus(kycStatus),
			VaultCount:     vaultCount,
			TotalDeposited: parsedTotalDeposited,
			CreatedAt:      createdAt.UTC(),
			UpdatedAt:      updatedAt.UTC(),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return out, total, nil
}

func buildVaultWhere(filter admindomain.VaultListFilter) (string, []any) {
	clauses := []string{"1=1"}
	args := make([]any, 0)

	if filter.Status != "" {
		args = append(args, strings.ToLower(strings.TrimSpace(filter.Status)))
		clauses = append(clauses, fmt.Sprintf("v.status = $%d", len(args)))
	}
	if filter.Search != "" {
		args = append(args, "%"+strings.TrimSpace(filter.Search)+"%")
		clauses = append(clauses, fmt.Sprintf("u.wallet_address ILIKE $%d", len(args)))
	}

	return strings.Join(clauses, " AND "), args
}

func buildSettlementWhere(filter admindomain.SettlementListFilter) (string, []any) {
	clauses := []string{"1=1"}
	args := make([]any, 0)

	if filter.Status != "" {
		args = append(args, strings.TrimSpace(filter.Status))
		clauses = append(clauses, fmt.Sprintf("s.status = $%d", len(args)))
	}
	if filter.Search != "" {
		args = append(args, "%"+strings.TrimSpace(filter.Search)+"%")
		clauses = append(clauses, fmt.Sprintf("u.wallet_address ILIKE $%d", len(args)))
	}
	if filter.DateFrom != nil {
		args = append(args, filter.DateFrom.UTC())
		clauses = append(clauses, fmt.Sprintf("s.created_at >= $%d", len(args)))
	}
	if filter.DateTo != nil {
		args = append(args, filter.DateTo.UTC())
		clauses = append(clauses, fmt.Sprintf("s.created_at <= $%d", len(args)))
	}

	return strings.Join(clauses, " AND "), args
}

func buildUserWhere(filter admindomain.UserListFilter) (string, []any) {
	clauses := []string{"1=1"}
	args := make([]any, 0)

	if filter.Search != "" {
		args = append(args, "%"+strings.TrimSpace(filter.Search)+"%")
		clauses = append(clauses, fmt.Sprintf("(u.wallet_address ILIKE $%d OR u.display_name ILIKE $%d)", len(args), len(args)))
	}

	return strings.Join(clauses, " AND "), args
}

func sanitizeOrder(order string) string {
	switch strings.ToLower(strings.TrimSpace(order)) {
	case "asc":
		return "ASC"
	default:
		return "DESC"
	}
}

func sanitizeVaultSort(sort string) string {
	switch strings.ToLower(strings.TrimSpace(sort)) {
	case "updated_at":
		return "v.updated_at"
	case "total_deposited":
		return "v.total_deposited"
	case "current_balance":
		return "v.current_balance"
	case "status":
		return "v.status"
	default:
		return "v.created_at"
	}
}

func sanitizeSettlementSort(sort string) string {
	switch strings.ToLower(strings.TrimSpace(sort)) {
	case "completed_at":
		return "s.completed_at"
	case "amount":
		return "s.amount"
	case "status":
		return "s.status"
	default:
		return "s.created_at"
	}
}

func sanitizeUserSort(sort string) string {
	switch strings.ToLower(strings.TrimSpace(sort)) {
	case "wallet_address":
		return "u.wallet_address"
	case "vault_count":
		return "vault_count"
	case "total_deposited":
		return "total_deposited"
	default:
		return "u.created_at"
	}
}
