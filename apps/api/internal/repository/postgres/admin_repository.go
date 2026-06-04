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

func (r *AdminRepository) GetVaultHealthDashboard(ctx context.Context) (admindomain.VaultHealthDashboardData, error) {
	const totalsQuery = `
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
	`

	var totalTVLStr string
	var totalDepositors int64
	if err := r.db.QueryRowContext(ctx, totalsQuery).Scan(&totalTVLStr, &totalDepositors); err != nil {
		return admindomain.VaultHealthDashboardData{}, err
	}

	totalTVL, err := decimal.NewFromString(totalTVLStr)
	if err != nil {
		return admindomain.VaultHealthDashboardData{}, fmt.Errorf("parse total_tvl: %w", err)
	}

	const vaultsQuery = `
		SELECT
			v.id,
			COALESCE(NULLIF(TRIM(u.display_name), ''), SUBSTRING(v.contract_address, 1, 12)) AS name,
			v.current_balance::text,
			v.status,
			COALESCE(dep.depositor_count, 0),
			COALESCE(pend.pending_count, 0),
			reb.last_rebalance_at,
			apy_current.realized_apy,
			apy_24h.realized_apy
		FROM vaults v
		JOIN users u ON u.id = v.user_id
		LEFT JOIN LATERAL (
			SELECT COUNT(DISTINCT COALESCE(vt.user_id, v.user_id)) AS depositor_count
			FROM vault_transactions vt
			WHERE vt.vault_id = v.id AND vt.type = 'deposit'
		) dep ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS pending_count
			FROM transactions t
			WHERE t.vault_id = v.id AND t.status = 'pending'
		) pend ON true
		LEFT JOIN LATERAL (
			SELECT MAX(a.allocated_at) AS last_rebalance_at
			FROM allocations a
			WHERE a.vault_id = v.id
		) reb ON true
		LEFT JOIN LATERAL (
			SELECT ah.realized_apy::text
			FROM apy_history ah
			WHERE ah.vault_id = v.id AND ah.period = '7d'
			ORDER BY ah.calculated_at DESC
			LIMIT 1
		) apy_current ON true
		LEFT JOIN LATERAL (
			SELECT ah.realized_apy::text
			FROM apy_history ah
			WHERE ah.vault_id = v.id AND ah.period = '7d'
				AND ah.calculated_at <= NOW() - INTERVAL '24 hours'
			ORDER BY ah.calculated_at DESC
			LIMIT 1
		) apy_24h ON true
		ORDER BY v.created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, vaultsQuery)
	if err != nil {
		return admindomain.VaultHealthDashboardData{}, err
	}
	defer rows.Close()

	vaultRows := make([]admindomain.VaultHealthRow, 0)
	for rows.Next() {
		var (
			id              string
			name            string
			tvlStr          string
			status          string
			depositors      int64
			pendingTx       int64
			lastRebalance   sql.NullTime
			apyCurrentStr   sql.NullString
			apy24hStr       sql.NullString
		)

		if err := rows.Scan(
			&id,
			&name,
			&tvlStr,
			&status,
			&depositors,
			&pendingTx,
			&lastRebalance,
			&apyCurrentStr,
			&apy24hStr,
		); err != nil {
			return admindomain.VaultHealthDashboardData{}, err
		}

		parsedID, err := uuid.Parse(id)
		if err != nil {
			return admindomain.VaultHealthDashboardData{}, err
		}
		tvl, err := decimal.NewFromString(tvlStr)
		if err != nil {
			return admindomain.VaultHealthDashboardData{}, err
		}

		var apy7d *decimal.Decimal
		if apyCurrentStr.Valid {
			parsed, err := decimal.NewFromString(apyCurrentStr.String)
			if err != nil {
				return admindomain.VaultHealthDashboardData{}, err
			}
			apy7d = &parsed
		}

		var apy7d24hAgo *decimal.Decimal
		if apy24hStr.Valid {
			parsed, err := decimal.NewFromString(apy24hStr.String)
			if err != nil {
				return admindomain.VaultHealthDashboardData{}, err
			}
			apy7d24hAgo = &parsed
		}

		var lastRebalanceAt *time.Time
		if lastRebalance.Valid {
			t := lastRebalance.Time.UTC()
			lastRebalanceAt = &t
		}

		vaultRows = append(vaultRows, admindomain.VaultHealthRow{
			ID:                  parsedID,
			Name:                name,
			TVL:                 tvl,
			APY7d:               apy7d,
			APY7d24hAgo:         apy7d24hAgo,
			Depositors:          depositors,
			PendingTransactions: pendingTx,
			LastRebalanceAt:     lastRebalanceAt,
			Status:              vault.VaultStatus(status),
		})
	}

	if err := rows.Err(); err != nil {
		return admindomain.VaultHealthDashboardData{}, err
	}

	return admindomain.VaultHealthDashboardData{
		TotalTVL:        totalTVL,
		TotalDepositors: totalDepositors,
		Vaults:          vaultRows,
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
	`, where, sortColumn, order, len(args)+1, len(args)+2) // #nosec G201 -- sortColumn/order come from sanitize* whitelist functions; values use $N placeholders

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
	`, where, sortColumn, order, len(args)+1, len(args)+2) // #nosec G201 -- sortColumn/order come from sanitize* whitelist functions; values use $N placeholders

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
	`, where, sortColumn, order, len(args)+1, len(args)+2) // #nosec G201 -- sortColumn/order come from sanitize* whitelist functions; values use $N placeholders

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
