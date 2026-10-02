package openapi

import (
	"strings"
	"testing"
)

func TestNewReflector_BuildsWithoutError(t *testing.T) {
	r, err := NewReflector()
	if err != nil {
		t.Fatalf("NewReflector() returned an error: %v", err)
	}
	if r == nil {
		t.Fatal("NewReflector() returned a nil reflector")
	}
}

func TestNewReflector_CoversTheDocumentedVaultRoutes(t *testing.T) {
	r, err := NewReflector()
	if err != nil {
		t.Fatalf("NewReflector(): %v", err)
	}

	yamlBytes, err := r.Spec.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML(): %v", err)
	}
	spec := string(yamlBytes)

	// One assertion per route this package claims to cover in its own doc
	// comment. If a route is added to spec.go without a matching addX
	// function actually calling AddOperation, this catches it; if the
	// package comment's claimed scope grows without the code catching up,
	// a human editing this test alongside it keeps the two in sync.
	wantPaths := []string{
		"/api/v1/vaults:",
		"/api/v1/vaults/{id}:",
		"/api/v1/vaults/{id}/deposit:",
		"/api/v1/vaults/{id}/withdraw:",
	}
	for _, p := range wantPaths {
		if !strings.Contains(spec, p) {
			t.Errorf("generated spec is missing path %q", p)
		}
	}
}

// TestNewReflector_PointerFieldsAreNullable_NonPointerFieldsAreNot is the
// regression test for the swaggest/jsonschema-go v0.3.78 caching bug
// documented on fixPointerNullability: vault.Vault has both plain
// decimal.Decimal fields (TotalDeposited, CurrentBalance, FeesPaid,
// YieldEarned — never null) and a *decimal.Decimal field (SoftCapacity —
// genuinely nullable) sharing the same underlying type. Confirmed by
// isolated reproduction that without the fix, whichever field the
// reflector's type cache reflects first "wins": the pointer field can
// silently lose its null marker, or — with an earlier, cruder attempt at
// the fix — the non-pointer fields can wrongly gain one. Both directions
// are asserted here so either regression fails this test.
func TestNewReflector_PointerFieldsAreNullable_NonPointerFieldsAreNot(t *testing.T) {
	r, err := NewReflector()
	if err != nil {
		t.Fatalf("NewReflector(): %v", err)
	}

	yamlBytes, err := r.Spec.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML(): %v", err)
	}
	spec := string(yamlBytes)

	nullableFields := []string{
		"soft_capacity",        // *decimal.Decimal
		"capacity_warning_pct", // *float64
		"last_harvested_at",    // *time.Time
		"deleted_at",           // *time.Time
	}
	for _, field := range nullableFields {
		if !fieldIsNullableInSpec(t, spec, field) {
			t.Errorf("field %q is a pointer in vault.Vault but the generated schema does not mark it nullable", field)
		}
	}

	nonNullableFields := []string{
		"total_deposited", // decimal.Decimal (plain) — shares a type with the *decimal.Decimal field above
		"current_balance",
		"fees_paid",
		"yield_earned",
	}
	for _, field := range nonNullableFields {
		if fieldIsNullableInSpec(t, spec, field) {
			t.Errorf("field %q is NOT a pointer in vault.Vault but the generated schema marks it nullable (the fix leaked into a field it shouldn't have touched)", field)
		}
	}
}

// fieldIsNullableInSpec is a deliberately simple textual check rather than a
// full YAML/JSON-Schema parse: it looks for the field's block containing
// `"null"` within a small window of lines, which is how both
// `type: ["string", "null"]`-style and the reflector's own
// multi-line-list-style output render. Good enough to catch the specific
// regression this test guards against without taking on a schema-walking
// dependency in a test file.
func fieldIsNullableInSpec(t *testing.T, spec, field string) bool {
	t.Helper()
	lines := strings.Split(spec, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != field+":" {
			continue
		}
		window := lines[i:min(i+5, len(lines))]
		if strings.Contains(strings.Join(window, "\n"), `"null"`) {
			return true
		}
	}
	return false
}
