package listquery_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/suncrestlabs/nester/apps/api/pkg/listquery"
)

func testSchema() listquery.ResourceSchema {
	return listquery.ResourceSchema{
		Fields: map[string]listquery.FieldSpec{
			"status": {
				Column:      "v.status",
				Type:        listquery.ValueEnum,
				AllowedOps:  []listquery.Operator{listquery.OpEq, listquery.OpIn},
				AllowedVals: map[string]bool{"active": true, "paused": true},
				Sortable:    true,
			},
			"apy": {
				Column:     "allocations.apy",
				Type:       listquery.ValueDecimal,
				AllowedOps: []listquery.Operator{listquery.OpGte, listquery.OpLte},
				Sortable:   true,
			},
			"created_at": {
				Column:     "v.created_at",
				Type:       listquery.ValueTime,
				AllowedOps: []listquery.Operator{listquery.OpGte, listquery.OpLte},
				Sortable:   true,
			},
		},
		DefaultSort:  "created_at",
		SearchColumn: "v.search_vector",
	}
}

func TestParseListQueryRejectsUnknownField(t *testing.T) {
	r := httptest.NewRequest("GET", "/?filter[nonexistent_field][eq]=x", nil)
	if _, err := listquery.ParseListQuery(r, testSchema()); err == nil {
		t.Fatal("expected error for unknown filter field")
	}
}

func TestParseListQueryRejectsDisallowedOperator(t *testing.T) {
	// "status" only allows eq/in, not gte.
	r := httptest.NewRequest("GET", "/?filter[status][gte]=active", nil)
	if _, err := listquery.ParseListQuery(r, testSchema()); err == nil {
		t.Fatal("expected error for disallowed operator")
	}
}

func TestParseListQueryRejectsInvalidEnumValue(t *testing.T) {
	r := httptest.NewRequest("GET", "/?filter[status][eq]=bogus", nil)
	if _, err := listquery.ParseListQuery(r, testSchema()); err == nil {
		t.Fatal("expected error for invalid enum value")
	}
}

func TestParseListQueryEqAndIn(t *testing.T) {
	r := httptest.NewRequest("GET", "/?filter[status][in]=active,paused&filter[apy][gte]=5", nil)
	pq, err := listquery.ParseListQuery(r, testSchema())
	if err != nil {
		t.Fatal(err)
	}
	if len(pq.Predicates) != 2 {
		t.Fatalf("expected 2 predicates, got %d", len(pq.Predicates))
	}
}

func TestParseListQuerySortMultiFieldAndDescending(t *testing.T) {
	r := httptest.NewRequest("GET", "/?sort=-apy,created_at", nil)
	pq, err := listquery.ParseListQuery(r, testSchema())
	if err != nil {
		t.Fatal(err)
	}
	if len(pq.Sort) != 2 || pq.Sort[0].Field != "apy" || pq.Sort[0].Order != "desc" || pq.Sort[1].Field != "created_at" || pq.Sort[1].Order != "asc" {
		t.Fatalf("unexpected sort result: %+v", pq.Sort)
	}
}

func TestParseListQuerySortRejectsNonSortableOrUnknownField(t *testing.T) {
	r := httptest.NewRequest("GET", "/?sort=not_a_field", nil)
	if _, err := listquery.ParseListQuery(r, testSchema()); err == nil {
		t.Fatal("expected error for unknown sort field")
	}
}

func TestParseListQuerySearchRejectedWhenUnsupported(t *testing.T) {
	schema := testSchema()
	schema.SearchColumn = ""
	r := httptest.NewRequest("GET", "/?q=stable", nil)
	if _, err := listquery.ParseListQuery(r, schema); err == nil {
		t.Fatal("expected error when search is unsupported for the resource")
	}
}

// TestBuildWhereAndOrderParameterizesValues proves a value containing SQL
// metacharacters is only ever carried as a bound parameter, never woven into
// the WHERE clause string itself.
func TestBuildWhereAndOrderParameterizesValues(t *testing.T) {
	malicious := `x'; DROP TABLE vaults;--`
	r := httptest.NewRequest("GET", "/?filter[status][eq]="+urlEscape(malicious), nil)
	// status is an enum field so it would reject "malicious" — use apy (decimal)
	// swapped for a string-typed field instead by building the query directly.
	schema := testSchema()
	schema.Fields["status"] = listquery.FieldSpec{
		Column:     "v.status",
		Type:       listquery.ValueString,
		AllowedOps: []listquery.Operator{listquery.OpEq},
		Sortable:   true,
	}
	pq, err := listquery.ParseListQuery(r, schema)
	if err != nil {
		t.Fatal(err)
	}
	where, args, _, err := listquery.BuildWhereAndOrder(pq, schema, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(where, malicious) {
		t.Fatalf("malicious value leaked into WHERE clause: %s", where)
	}
	found := false
	for _, a := range args {
		if s, ok := a.(string); ok && s == malicious {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected malicious value to appear verbatim in args, got %+v", args)
	}
}

// TestBuildWhereAndOrderResolvesColumnFromAllowlist proves the SQL column
// name comes from the schema's Column mapping, not the client-facing field
// name, by using a schema where they deliberately differ.
func TestBuildWhereAndOrderResolvesColumnFromAllowlist(t *testing.T) {
	r := httptest.NewRequest("GET", "/?filter[apy][gte]=5", nil)
	schema := testSchema()
	pq, err := listquery.ParseListQuery(r, schema)
	if err != nil {
		t.Fatal(err)
	}
	where, _, _, err := listquery.BuildWhereAndOrder(pq, schema, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(where, "allocations.apy") {
		t.Fatalf("expected WHERE clause to reference allowlisted column allocations.apy, got: %s", where)
	}
	if strings.Contains(where, "\"apy\"") {
		t.Fatalf("client-facing field name should not appear as a raw identifier: %s", where)
	}
}

func TestBuildWhereAndOrderOrderClause(t *testing.T) {
	r := httptest.NewRequest("GET", "/?sort=-apy", nil)
	schema := testSchema()
	pq, err := listquery.ParseListQuery(r, schema)
	if err != nil {
		t.Fatal(err)
	}
	_, _, order, err := listquery.BuildWhereAndOrder(pq, schema, 0)
	if err != nil {
		t.Fatal(err)
	}
	if order != "allocations.apy DESC" {
		t.Fatalf("unexpected order clause: %q", order)
	}
}

func urlEscape(s string) string {
	// Minimal query-escape sufficient for test fixtures (no external import).
	var b strings.Builder
	for _, r := range s {
		switch r {
		case ' ':
			b.WriteString("%20")
		case ';':
			b.WriteString("%3B")
		case '\'':
			b.WriteString("%27")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
