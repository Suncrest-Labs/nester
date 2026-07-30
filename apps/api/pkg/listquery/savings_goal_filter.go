package listquery

import (
	"net/http"
	"strings"
)

// savingsGoalSchema is the allowlist for GET /api/v1/savings-goals:
// filter[field][op]=value, sort=-field, q=term. category/status stay
// ValueString here — their real enum validation (GoalCategory/GoalStatus)
// lives in SavingsGoalService and is applied after this generic parse.
var savingsGoalSchema = ResourceSchema{
	Fields: map[string]FieldSpec{
		"category": {
			Column:     "category",
			Type:       ValueString,
			AllowedOps: []Operator{OpEq},
		},
		"status": {
			Column:     "status",
			Type:       ValueString,
			AllowedOps: []Operator{OpEq},
		},
		"created_at": {
			Column:   "created_at",
			Sortable: true,
		},
		"target_amount": {
			Column:   "target_amount",
			Sortable: true,
		},
		"deadline": {
			Column:   "deadline",
			Sortable: true,
		},
	},
	DefaultSort:  "created_at",
	SearchColumn: "search_vector",
}

// SavingsGoalListParams combines pagination, sort, and savings-goal filters.
type SavingsGoalListParams struct {
	Page            PageParams
	Sort            SortParams
	Category        string
	Status          string
	IncludeArchived bool
	Search          string
}

// ParseSavingsGoalList reads list query parameters for GET
// /api/v1/savings-goals, using the shared filter[field][op]=value / sort= /
// q= grammar, plus the existing include_archived boolean toggle (a display
// modifier, not a column filter, so it stays outside the bracket grammar).
//
// This endpoint previously accepted flat ?category=&status= params (with no
// grammar at all); those are still honored as a fallback when the
// bracket-style filter isn't present, so existing callers keep working.
func ParseSavingsGoalList(r *http.Request) (SavingsGoalListParams, error) {
	pq, err := ParseListQuery(r, savingsGoalSchema)
	if err != nil {
		return SavingsGoalListParams{}, err
	}

	q := r.URL.Query()
	params := SavingsGoalListParams{
		Page:            pq.Page,
		Sort:            SortParams{Field: savingsGoalSchema.DefaultSort, Order: "desc"},
		Search:          pq.Search,
		IncludeArchived: q.Get("include_archived") == "true",
		Category:        strings.TrimSpace(q.Get("category")),
		Status:          strings.TrimSpace(q.Get("status")),
	}
	if len(pq.Sort) > 0 {
		params.Sort = pq.Sort[0]
	}

	for _, p := range pq.Predicates {
		switch p.Field {
		case "category":
			params.Category, _ = p.Value.(string)
		case "status":
			params.Status, _ = p.Value.(string)
		}
	}

	return params, nil
}
