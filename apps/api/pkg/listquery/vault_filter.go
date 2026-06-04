package listquery

import (
	"net/http"
	"strings"
	"time"
)

var vaultSortFields = map[string]bool{
	"created_at":      true,
	"updated_at":      true,
	"current_balance": true,
	"status":          true,
}

// VaultListParams combines pagination, sort, and vault-specific filters.
type VaultListParams struct {
	Page         PageParams
	Sort         SortParams
	Status       string
	Currency     string
	MinBalance   *string
	CreatedAfter *time.Time
}

// ParseVaultList reads list query parameters for GET /api/v1/vaults.
func ParseVaultList(r *http.Request) (VaultListParams, error) {
	page, err := ParsePage(r)
	if err != nil {
		return VaultListParams{}, err
	}
	sort, err := ParseSort(r, "created_at", vaultSortFields)
	if err != nil {
		return VaultListParams{}, err
	}

	createdAfter, err := ParseTimeQueryStart(r, "created_after")
	if err != nil {
		return VaultListParams{}, err
	}

	minBalance, err := ParseDecimalQuery(r, "min_balance")
	if err != nil {
		return VaultListParams{}, err
	}

	return VaultListParams{
		Page:         page,
		Sort:         sort,
		Status:       strings.TrimSpace(r.URL.Query().Get("status")),
		Currency:     strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("currency"))),
		MinBalance:   minBalance,
		CreatedAfter: createdAfter,
	}, nil
}
