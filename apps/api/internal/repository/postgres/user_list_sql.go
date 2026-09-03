package postgres

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
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
	if filter.Search != "" {
		args = append(args, filter.Search)
		clauses = append(clauses, fmt.Sprintf("search_vector @@ plainto_tsquery('english', $%d)", len(args)))
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
