package postgres

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/offramp"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/pkg/listquery"
)

func buildUserVaultWhere(userID uuid.UUID, filter vault.UserListFilter) (string, []any) {
	clauses := []string{"user_id = $1", "deleted_at IS NULL"}
	args := []any{userID.String()}

	if filter.Status != "" {
		args = append(args, strings.ToLower(strings.TrimSpace(filter.Status)))
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if filter.Currency != "" {
		args = append(args, strings.ToUpper(strings.TrimSpace(filter.Currency)))
		clauses = append(clauses, fmt.Sprintf("currency = $%d", len(args)))
	}
	if filter.MinBalance != nil {
		args = append(args, strings.TrimSpace(*filter.MinBalance))
		clauses = append(clauses, fmt.Sprintf("current_balance >= $%d::numeric", len(args)))
	}
	if filter.CreatedAfter != nil {
		args = append(args, filter.CreatedAfter.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", len(args)))
	}

	return strings.Join(clauses, " AND "), args
}

func sanitizeUserVaultSort(field string) string {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "updated_at":
		return "updated_at"
	case "current_balance":
		return "current_balance"
	case "status":
		return "status"
	default:
		return "created_at"
	}
}

func buildUserSettlementWhere(userID uuid.UUID, filter offramp.UserListFilter) (string, []any) {
	clauses := []string{"user_id = $1"}
	args := []any{userID.String()}

	if filter.Status != "" {
		args = append(args, strings.TrimSpace(filter.Status))
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if filter.DateFrom != nil {
		args = append(args, filter.DateFrom.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if filter.DateTo != nil {
		args = append(args, filter.DateTo.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at <= $%d", len(args)))
	}
	if filter.MinAmount != nil {
		args = append(args, strings.TrimSpace(*filter.MinAmount))
		clauses = append(clauses, fmt.Sprintf("amount >= $%d::numeric", len(args)))
	}
	if filter.DestinationProvider != "" {
		args = append(args, strings.TrimSpace(filter.DestinationProvider))
		clauses = append(clauses, fmt.Sprintf("destination_provider = $%d", len(args)))
	}
	if filter.FiatCurrency != "" {
		args = append(args, strings.ToUpper(strings.TrimSpace(filter.FiatCurrency)))
		clauses = append(clauses, fmt.Sprintf("fiat_currency = $%d", len(args)))
	}

	return strings.Join(clauses, " AND "), args
}

func sanitizeUserSettlementSort(field string) string {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "completed_at":
		return "completed_at"
	case "amount":
		return "amount"
	case "status":
		return "status"
	default:
		return "created_at"
	}
}

func settlementKeysetClause(cursor listquery.SettlementCursor, args []any) (string, []any) {
	args = append(args, cursor.CreatedAt.UTC(), cursor.ID.String())
	n := len(args)
	return fmt.Sprintf("(created_at, id) < ($%d, $%d)", n-1, n), args
}
