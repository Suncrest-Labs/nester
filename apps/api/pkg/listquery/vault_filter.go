package listquery

import (
	"net/http"
	"strings"
	"time"
)

// vaultSchema is the allowlist for GET /api/v1/vaults: filter[field][op]=value,
// sort=-field, q=term. Status/currency/created_at only support eq/gte today
// because VaultListParams (and the vault.UserListFilter it feeds) carry a
// single value per field, not a list — "in" isn't wired through yet.
var vaultSchema = ResourceSchema{
	Fields: map[string]FieldSpec{
		"status": {
			Column:      "status",
			Type:        ValueEnum,
			AllowedOps:  []Operator{OpEq},
			AllowedVals: map[string]bool{"active": true, "paused": true, "closed": true},
			Sortable:    true,
		},
		"currency": {
			Column:     "currency",
			Type:       ValueString,
			AllowedOps: []Operator{OpEq},
		},
		"current_balance": {
			Column:     "current_balance",
			Type:       ValueDecimal,
			AllowedOps: []Operator{OpGte},
			Sortable:   true,
		},
		"created_at": {
			Column:     "created_at",
			Type:       ValueTime,
			AllowedOps: []Operator{OpGte},
			Sortable:   true,
		},
		"updated_at": {
			Column:   "updated_at",
			Sortable: true,
		},
	},
	DefaultSort:  "created_at",
	SearchColumn: "search_vector",
}

// VaultListParams combines pagination, sort, and vault-specific filters.
type VaultListParams struct {
	Page         PageParams
	Sort         SortParams
	Status       string
	Currency     string
	MinBalance   *string
	CreatedAfter *time.Time
	Search       string
}

// ParseVaultList reads list query parameters for GET /api/v1/vaults, using
// the shared filter[field][op]=value / sort= / q= grammar.
func ParseVaultList(r *http.Request) (VaultListParams, error) {
	pq, err := ParseListQuery(r, vaultSchema)
	if err != nil {
		return VaultListParams{}, err
	}

	params := VaultListParams{
		Page:   pq.Page,
		Sort:   SortParams{Field: vaultSchema.DefaultSort, Order: "desc"},
		Search: pq.Search,
	}
	if len(pq.Sort) > 0 {
		params.Sort = pq.Sort[0]
	}

	for _, p := range pq.Predicates {
		switch p.Field {
		case "status":
			params.Status, _ = p.Value.(string)
		case "currency":
			v, _ := p.Value.(string)
			params.Currency = strings.ToUpper(v)
		case "current_balance":
			v, _ := p.Value.(string)
			params.MinBalance = &v
		case "created_at":
			v, _ := p.Value.(string)
			t, _, err := parseFlexibleTime(v)
			if err != nil {
				return VaultListParams{}, err
			}
			utc := t.UTC()
			params.CreatedAfter = &utc
		}
	}

	return params, nil
}
