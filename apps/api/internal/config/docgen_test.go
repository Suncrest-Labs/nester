package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestConfigDoc_CoversEveryEnvKeyInConfigGo(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "config.go", nil, 0)
	if err != nil {
		t.Fatalf("parse config.go: %v", err)
	}

	loaderMethods := map[string]bool{
		"requiredString":     true,
		"requiredURL":        true,
		"stringDefault":      true,
		"uuidDefault":        true,
		"intDefault":         true,
		"floatDefault":       true,
		"floatOverride":      true,
		"intOverride":        true,
		"durationOverride":   true,
		"stringSliceDefault": true,
		"intSliceDefault":    true,
		"boolDefault":        true,
		"durationDefault":    true,
		"lookup":             true,
	}

	discovered := make(map[string]bool)
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !loaderMethods[sel.Sel.Name] {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		envName, err := strconv.Unquote(lit.Value)
		if err == nil && envName != "" {
			discovered[envName] = true
		}
		return true
	})

	if len(discovered) < 100 {
		t.Fatalf("expected at least 100 env vars discovered in config.go, got %d", len(discovered))
	}

	documented := make(map[string]EnvVarSpec)
	for _, spec := range EnvSpecs() {
		if _, dup := documented[spec.Name]; dup {
			t.Errorf("duplicate EnvVarSpec entry for %q", spec.Name)
		}
		if spec.Type == "" || spec.Default == "" || spec.MainnetConstraint == "" {
			t.Errorf("EnvVarSpec %q has empty required metadata field: %+v", spec.Name, spec)
		}
		documented[spec.Name] = spec
	}

	for envName := range discovered {
		if _, ok := documented[envName]; !ok {
			t.Errorf("env var %q is read in config.go but missing from EnvSpecs() in docgen.go", envName)
		}
	}

	for envName := range documented {
		if !discovered[envName] {
			t.Errorf("env var %q is in EnvSpecs() but no longer read in config.go", envName)
		}
	}
}

func TestConfigDoc_MatchesCommittedMarkdown(t *testing.T) {
	docPath := filepath.Join("..", "..", "..", "..", "docs", "api-config.md")
	raw, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v (run: go run ./cmd/config-doc -write ../../docs/api-config.md)", docPath, err)
	}

	committed := strings.ReplaceAll(string(raw), "\r\n", "\n")
	generated := GenerateMarkdownReference()

	if committed != generated {
		t.Fatalf("%s is out of date with internal/config/docgen.go; regenerate with: cd apps/api && go run ./cmd/config-doc -write ../../docs/api-config.md", docPath)
	}
}
