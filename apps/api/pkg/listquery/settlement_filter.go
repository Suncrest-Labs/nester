package listquery

import (
	"net/http"
	"strings"
	"time"
)

// settlementSchema is the allowlist for GET /api/v1/settlements:
// filter[field][op]=value, sort=-field, q=term.
var settlementSchema = ResourceSchema{
	Fields: map[string]FieldSpec{
		"status": {
			Column: "status",
			Type:   ValueEnum,
			AllowedOps: []Operator{OpEq},
			AllowedVals: map[string]bool{
				"initiated":         true,
				"liquidity_matched": true,
				"fiat_dispatched":   true,
				"confirmed":         true,
				"failed":            true,
			},
			Sortable: true,
		},
		"created_at": {
			Column:     "created_at",
			Type:       ValueTime,
			AllowedOps: []Operator{OpGte, OpLte},
			Sortable:   true,
		},
		"completed_at": {
			Column:   "completed_at",
			Sortable: true,
		},
		"amount": {
			Column:     "amount",
			Type:       ValueDecimal,
			AllowedOps: []Operator{OpGte},
			Sortable:   true,
		},
		"destination_provider": {
			Column:     "destination_provider",
			Type:       ValueString,
			AllowedOps: []Operator{OpEq},
		},
		"fiat_currency": {
			Column:     "fiat_currency",
			Type:       ValueString,
			AllowedOps: []Operator{OpEq},
		},
	},
	DefaultSort:  "created_at",
	SearchColumn: "search_vector",
}

// SettlementListParams combines pagination, sort, and settlement filters.
type SettlementListParams struct {
	Page                PageParams
	Sort                SortParams
	Status              string
	DateFrom            *time.Time
	DateTo              *time.Time
	MinAmount           *string
	DestinationProvider string
	FiatCurrency        string
	Search              string
}

// ParseSettlementList reads list query parameters for GET /api/v1/settlements,
// using the shared filter[field][op]=value / sort= / q= grammar.
func ParseSettlementList(r *http.Request) (SettlementListParams, error) {
	pq, err := ParseListQuery(r, settlementSchema)
	if err != nil {
		return SettlementListParams{}, err
	}

	params := SettlementListParams{
		Page:   pq.Page,
		Sort:   SortParams{Field: settlementSchema.DefaultSort, Order: "desc"},
		Search: pq.Search,
	}
	if len(pq.Sort) > 0 {
		params.Sort = pq.Sort[0]
	}

	for _, p := range pq.Predicates {
		switch p.Field {
		case "status":
			params.Status, _ = p.Value.(string)
		case "created_at":
			v, _ := p.Value.(string)
			t, inclusiveEnd, err := parseFlexibleTime(v)
			if err != nil {
				return SettlementListParams{}, err
			}
			switch p.Op {
			case OpGte:
				start := t.UTC()
				params.DateFrom = &start
			case OpLte:
				end := t
				if inclusiveEnd {
					end = t.Add(24*time.Hour - time.Nanosecond)
				}
				end = end.UTC()
				params.DateTo = &end
			}
		case "amount":
			v, _ := p.Value.(string)
			params.MinAmount = &v
		case "destination_provider":
			params.DestinationProvider, _ = p.Value.(string)
		case "fiat_currency":
			v, _ := p.Value.(string)
			params.FiatCurrency = strings.ToUpper(v)
		}
	}

	return params, nil
}
