package listquery

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Operator is a typed filter operator accepted by the bracket-style grammar
// filter[field][op]=value, e.g. filter[apy][gte]=5.
type Operator string

const (
	OpEq       Operator = "eq"
	OpIn       Operator = "in"
	OpGte      Operator = "gte"
	OpLte      Operator = "lte"
	OpContains Operator = "contains"
)

// FieldValueType controls how a filter value is parsed and cast in SQL.
type FieldValueType int

const (
	ValueString FieldValueType = iota
	ValueEnum
	ValueDecimal
	ValueTime
	ValueUUID
)

// FieldSpec is one allowlisted field. Column is the real SQL identifier and
// is the only thing ever interpolated into a query for this field — the
// client-facing field name never is. This allowlist is the security and
// performance boundary: a field absent here can never reach SQL, so it can
// never trigger an unindexed scan or reference a column it shouldn't.
type FieldSpec struct {
	Column      string
	Type        FieldValueType
	AllowedOps  []Operator
	AllowedVals map[string]bool // required when Type == ValueEnum
	Sortable    bool
}

// ResourceSchema is a per-endpoint allowlist: which fields can be filtered
// and/or sorted, and which generated tsvector column backs q= full-text
// search (empty disables search for the resource).
type ResourceSchema struct {
	Fields       map[string]FieldSpec
	DefaultSort  string
	SearchColumn string
}

// Predicate is one resolved, type-checked filter condition. Value is a
// string for eq/gte/lte/contains, []string for in.
type Predicate struct {
	Field string
	Op    Operator
	Value any
}

// ParsedQuery is the generic result of parsing one request's filter[...],
// sort=, q=, and page/cursor parameters against a ResourceSchema.
type ParsedQuery struct {
	Predicates []Predicate
	Sort       []SortParams
	Search     string
	Page       PageParams
}

var filterKeyRe = regexp.MustCompile(`^filter\[([A-Za-z0-9_]+)\]\[([A-Za-z0-9_]+)\]$`)

// ParseListQuery parses filter[field][op]=value, sort=-field,field2, q=term,
// and page/per_page/cursor against schema. Any field or operator not present
// in the allowlist is rejected outright — never silently dropped.
func ParseListQuery(r *http.Request, schema ResourceSchema) (ParsedQuery, error) {
	page, err := ParsePage(r)
	if err != nil {
		return ParsedQuery{}, err
	}

	q := r.URL.Query()

	preds, err := parseFilterParams(q, schema)
	if err != nil {
		return ParsedQuery{}, err
	}

	sortParams, err := parseSortParam(strings.TrimSpace(q.Get("sort")), schema)
	if err != nil {
		return ParsedQuery{}, err
	}

	search := strings.TrimSpace(q.Get("q"))
	if search != "" && schema.SearchColumn == "" {
		return ParsedQuery{}, fmt.Errorf("%w: full-text search is not supported on this resource", ErrInvalidQuery)
	}

	return ParsedQuery{Predicates: preds, Sort: sortParams, Search: search, Page: page}, nil
}

func parseFilterParams(q url.Values, schema ResourceSchema) ([]Predicate, error) {
	var preds []Predicate
	for key, vals := range q {
		m := filterKeyRe.FindStringSubmatch(key)
		if m == nil {
			continue
		}
		field, opStr := m[1], m[2]
		spec, ok := schema.Fields[field]
		if !ok {
			return nil, fmt.Errorf("%w: unknown filter field %q", ErrInvalidQuery, field)
		}
		op := Operator(opStr)
		if !operatorAllowed(spec.AllowedOps, op) {
			return nil, fmt.Errorf("%w: operator %q not allowed on field %q", ErrInvalidQuery, opStr, field)
		}
		if len(vals) == 0 || strings.TrimSpace(vals[0]) == "" {
			return nil, fmt.Errorf("%w: filter[%s][%s] requires a value", ErrInvalidQuery, field, opStr)
		}
		value, err := coerceFilterValue(spec, op, vals[0])
		if err != nil {
			return nil, err
		}
		preds = append(preds, Predicate{Field: field, Op: op, Value: value})
	}
	// Deterministic order so the same request always builds the same SQL
	// string (stable for tests and query-plan caching).
	sort.Slice(preds, func(i, j int) bool { return preds[i].Field < preds[j].Field })
	return preds, nil
}

func operatorAllowed(allowed []Operator, op Operator) bool {
	for _, a := range allowed {
		if a == op {
			return true
		}
	}
	return false
}

func coerceFilterValue(spec FieldSpec, op Operator, raw string) (any, error) {
	if op == OpIn {
		parts := strings.Split(raw, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if err := validateScalar(spec, p); err != nil {
				return nil, err
			}
			out = append(out, p)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("%w: filter value must not be empty", ErrInvalidQuery)
		}
		return out, nil
	}
	raw = strings.TrimSpace(raw)
	if err := validateScalar(spec, raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func validateScalar(spec FieldSpec, val string) error {
	switch spec.Type {
	case ValueEnum:
		if !spec.AllowedVals[val] {
			return fmt.Errorf("%w: %q is not a valid value for this field", ErrInvalidQuery, val)
		}
	case ValueDecimal:
		if _, err := decimal.NewFromString(val); err != nil {
			return fmt.Errorf("%w: value must be a valid decimal number", ErrInvalidQuery)
		}
	case ValueUUID:
		if _, err := uuid.Parse(val); err != nil {
			return fmt.Errorf("%w: value must be a valid UUID", ErrInvalidQuery)
		}
	case ValueTime:
		if _, _, err := parseFlexibleTime(val); err != nil {
			return fmt.Errorf("%w: value must be RFC3339 or YYYY-MM-DD", ErrInvalidQuery)
		}
	case ValueString:
		// No further validation — parameterized in SQL either way.
	}
	return nil
}

func parseSortParam(raw string, schema ResourceSchema) ([]SortParams, error) {
	if raw == "" {
		return []SortParams{{Field: schema.DefaultSort, Order: "desc"}}, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]SortParams, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		order := "asc"
		field := part
		if strings.HasPrefix(part, "-") {
			order = "desc"
			field = part[1:]
		}
		spec, ok := schema.Fields[field]
		if !ok || !spec.Sortable {
			return nil, fmt.Errorf("%w: invalid sort field %q", ErrInvalidQuery, field)
		}
		out = append(out, SortParams{Field: field, Order: order})
	}
	if len(out) == 0 {
		return []SortParams{{Field: schema.DefaultSort, Order: "desc"}}, nil
	}
	return out, nil
}

// BuildWhereAndOrder turns a ParsedQuery into a parameterized SQL WHERE
// fragment (clauses joined with AND, empty string if there are no
// predicates or search term) plus an ORDER BY fragment (without the
// "ORDER BY" keyword). args starts empty; placeholders begin at
// $startArgIdx+1 so callers can merge onto their own existing arg slice
// (e.g. a base "user_id = $1" clause built before calling this).
func BuildWhereAndOrder(pq ParsedQuery, schema ResourceSchema, startArgIdx int) (whereClause string, args []any, orderClause string, err error) {
	var clauses []string
	n := startArgIdx

	for _, p := range pq.Predicates {
		spec, ok := schema.Fields[p.Field]
		if !ok {
			// Unreachable in practice: ParseListQuery only ever emits
			// predicates for fields present in schema. Guarded anyway since
			// callers may construct a ParsedQuery by hand (e.g. tests).
			return "", nil, "", fmt.Errorf("%w: unknown filter field %q", ErrInvalidQuery, p.Field)
		}
		switch p.Op {
		case OpEq:
			n++
			clauses = append(clauses, fmt.Sprintf("%s = $%d%s", spec.Column, n, sqlCastSuffix(spec.Type))) // #nosec G201 -- spec.Column resolved only from the schema allowlist, never from client input; value is a $N placeholder
			args = append(args, p.Value)
		case OpIn:
			vals, _ := p.Value.([]string)
			placeholders := make([]string, len(vals))
			for i, v := range vals {
				n++
				placeholders[i] = fmt.Sprintf("$%d", n)
				args = append(args, v)
			}
			clauses = append(clauses, fmt.Sprintf("%s IN (%s)", spec.Column, strings.Join(placeholders, ", "))) // #nosec G201 -- spec.Column resolved only from the schema allowlist, never from client input; values are $N placeholders
		case OpGte:
			n++
			clauses = append(clauses, fmt.Sprintf("%s >= $%d%s", spec.Column, n, sqlCastSuffix(spec.Type))) // #nosec G201 -- spec.Column resolved only from the schema allowlist, never from client input; value is a $N placeholder
			args = append(args, p.Value)
		case OpLte:
			n++
			clauses = append(clauses, fmt.Sprintf("%s <= $%d%s", spec.Column, n, sqlCastSuffix(spec.Type))) // #nosec G201 -- spec.Column resolved only from the schema allowlist, never from client input; value is a $N placeholder
			args = append(args, p.Value)
		case OpContains:
			n++
			val, _ := p.Value.(string)
			clauses = append(clauses, fmt.Sprintf("%s ILIKE $%d", spec.Column, n)) // #nosec G201 -- spec.Column resolved only from the schema allowlist, never from client input; value is a $N placeholder
			args = append(args, "%"+val+"%")
		default:
			return "", nil, "", fmt.Errorf("%w: unsupported operator %q", ErrInvalidQuery, p.Op)
		}
	}

	if pq.Search != "" {
		if schema.SearchColumn == "" {
			return "", nil, "", fmt.Errorf("%w: full-text search is not supported on this resource", ErrInvalidQuery)
		}
		n++
		clauses = append(clauses, fmt.Sprintf("%s @@ plainto_tsquery('english', $%d)", schema.SearchColumn, n)) // #nosec G201 -- schema.SearchColumn is a fixed server-side constant, never client input
		args = append(args, pq.Search)
	}

	orderParts := make([]string, 0, len(pq.Sort))
	for _, s := range pq.Sort {
		spec, ok := schema.Fields[s.Field]
		if !ok || !spec.Sortable {
			return "", nil, "", fmt.Errorf("%w: invalid sort field %q", ErrInvalidQuery, s.Field)
		}
		orderParts = append(orderParts, fmt.Sprintf("%s %s", spec.Column, sanitizeOrderKeyword(s.Order))) // #nosec G201 -- spec.Column resolved only from the schema allowlist, never from client input; order is normalized to a fixed ASC/DESC keyword
	}

	return strings.Join(clauses, " AND "), args, strings.Join(orderParts, ", "), nil
}

func sqlCastSuffix(t FieldValueType) string {
	switch t {
	case ValueDecimal:
		return "::numeric"
	case ValueTime:
		return "::timestamptz"
	default:
		return ""
	}
}

func sanitizeOrderKeyword(order string) string {
	if strings.EqualFold(order, "asc") {
		return "ASC"
	}
	return "DESC"
}
