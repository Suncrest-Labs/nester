package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Amount-bearing struct fields must never be represented as a binary floating
// point type. Stellar amounts are i128 stroops; float64 carries 53 bits of
// mantissa and silently loses precision well before that range is exhausted.
// See issue #1121 and the column widening done in #1074.
//
// This guard is deliberately name-driven rather than type-driven. Plenty of
// legitimate non-monetary values in these packages are float64 — APY
// percentages, deterioration z-scores, allocation ratios, capacity warning
// thresholds. Banning the float64 identifier outright flags all of those and
// says nothing about precision on the money path.
var amountFieldNames = map[string]bool{
	"amount":         true,
	"balance":        true,
	"currentbalance": true,
	"totaldeposited": true,
	"totalwithdrawn": true,
	"yieldearned":    true,
	"feespaid":       true,
	"fee":            true,
	"principal":      true,
	"deposit":        true,
	"withdrawal":     true,
	"grossamount":    true,
	"netamount":      true,
	"softcapacity":   true,
	"stroops":        true,
}

// floatTypes are the representations that cannot hold an exact decimal amount.
var floatTypes = map[string]bool{
	"float32": true,
	"float64": true,
}

// exemptFiles hold advisory projections rather than settled amounts. The
// savings planner returns a forecast produced by the intelligence service; it
// never represents a real balance, is never written to the ledger and never
// crosses the contract boundary, so decimal precision is not meaningful there.
var exemptFiles = map[string]bool{
	filepath.Join("..", "domain", "intelligence", "model.go"): true,
}

func TestNoFloatOnAmountPath(t *testing.T) {
	for _, dir := range []string{
		filepath.Join("..", "domain"),
		filepath.Join("..", "service"),
		filepath.Join("..", "handler"),
	} {
		checkAmountFieldsInDir(t, dir)
	}
}

func checkAmountFieldsInDir(t *testing.T, dir string) {
	t.Helper()

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatalf("guarded directory %s does not exist", dir)
	}

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}
		if strings.HasSuffix(info.Name(), "_test.go") {
			return nil
		}
		if exemptFiles[path] {
			return nil
		}
		checkAmountFieldsInFile(t, path)
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk %s: %v", dir, err)
	}
}

func checkAmountFieldsInFile(t *testing.T, path string) {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("failed to parse %s: %v", path, err)
	}

	ast.Inspect(f, func(n ast.Node) bool {
		st, ok := n.(*ast.StructType)
		if !ok || st.Fields == nil {
			return true
		}
		for _, field := range st.Fields.List {
			typeName := floatTypeName(field.Type)
			if typeName == "" {
				continue
			}
			for _, name := range field.Names {
				if amountFieldNames[strings.ToLower(name.Name)] {
					t.Errorf(
						"%s: field %s is %s; amounts must use decimal.Decimal to preserve the full i128 stroop range",
						fset.Position(name.Pos()), name.Name, typeName,
					)
				}
			}
		}
		return true
	})
}

// floatTypeName reports the underlying float type name for an expression,
// unwrapping pointers and slices, and returns "" when the expression is not a
// float type.
func floatTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		if floatTypes[e.Name] {
			return e.Name
		}
	case *ast.StarExpr:
		return floatTypeName(e.X)
	case *ast.ArrayType:
		return floatTypeName(e.Elt)
	}
	return ""
}
