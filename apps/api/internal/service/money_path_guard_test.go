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

func TestNoFloat64OnAmountPath(t *testing.T) {
	// Enforce that float64 is not used anywhere in domain or service amount logic.
	root := filepath.Join("..", "domain")
	svcRoot := filepath.Join("..", "service")

	checkDir(t, root)
	checkDir(t, svcRoot)
}

func checkDir(t *testing.T, dir string) {
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".go") {
			verifyNoFloat64(t, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk %s: %v", dir, err)
	}
}

func verifyNoFloat64(t *testing.T, path string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("failed to parse %s: %v", path, err)
	}

	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if ok {
			if ident, ok := sel.X.(*ast.Ident); ok {
				if ident.Name == "builtin" && sel.Sel.Name == "float64" {
					t.Errorf("%s: forbidden float64 usage detected", fset.Position(n.Pos()))
				}
			}
		}
		ident, ok := n.(*ast.Ident)
		if ok && ident.Name == "float64" {
			t.Errorf("%s: forbidden float64 type/identifier detected", fset.Position(n.Pos()))
		}
		return true
	})
}
