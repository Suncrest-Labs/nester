package middleware

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// This file enforces the "adding a route without adding a matrix entry fails
// the test" criterion of #1101.
//
// The route list is recovered from the handler sources rather than from a
// hand-maintained list, because a hand-maintained list is the very thing that
// goes stale. Instantiating the real handlers would be more direct, but every
// constructor wants a database; parsing the registrations gets the same
// answer without one.

// handlerPkgDir is the directory holding the HTTP handlers, relative to this
// package.
const handlerPkgDir = "../handler"

// registrarNames are the mux methods that bind a pattern to a handler.
var registrarNames = map[string]bool{
	"HandleFunc": true,
	"Handle":     true,
}

// muxPattern is "METHOD /path" or just "/path".
var muxPattern = regexp.MustCompile(`^(?:([A-Z]+)\s+)?(/\S*)$`)

// discoveredRoute is a route the server registers.
type discoveredRoute struct {
	Method string
	Path   string
	// Source is file:line, so a failure points at the registration.
	Source string
}

// discoverRegisteredRoutes parses the handler package and returns every route
// registered through a mux.
func discoverRegisteredRoutes(t *testing.T) []discoveredRoute {
	t.Helper()

	entries, err := os.ReadDir(handlerPkgDir)
	if err != nil {
		t.Fatalf("read handler dir: %v", err)
	}

	fset := token.NewFileSet()
	var routes []discoveredRoute

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(handlerPkgDir, name)
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !registrarNames[sel.Sel.Name] {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			raw, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			m := muxPattern.FindStringSubmatch(strings.TrimSpace(raw))
			if m == nil {
				return true
			}
			pos := fset.Position(lit.Pos())
			routes = append(routes, discoveredRoute{
				Method: m[1],
				Path:   m[2],
				Source: filepath.Base(pos.Filename) + ":" + strconv.Itoa(pos.Line),
			})
			return true
		})
	}

	if len(routes) == 0 {
		t.Fatalf("discovered no routes under %s — the parser is broken, not the matrix", handlerPkgDir)
	}
	return routes
}

// normalizePattern rewrites a route to a comparable shape: wildcard segments
// collapse to "{}", so "/vaults/{id}" from the source and
// "/vaults/0000-..." from the matrix compare equal.
func normalizePattern(method, path string) string {
	path = strings.TrimSuffix(path, "{$}")
	segs := strings.Split(strings.Trim(path, "/"), "/")
	for i, s := range segs {
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			segs[i] = "{}"
		}
	}
	if method == "" {
		method = "ANY"
	}
	return method + " /" + strings.Join(segs, "/")
}

// matrixKeys indexes the matrix by normalized pattern. Concrete IDs in matrix
// paths are mapped to "{}" positionally against the discovered routes, so the
// matrix keeps using real-looking URLs while still matching.
func matrixKeys(discovered []discoveredRoute) map[string]bool {
	// Index discovered routes by method + segment count so a matrix path can
	// find its template and learn which segments are wildcards.
	type shape struct {
		method string
		n      int
	}
	templates := map[shape][][]string{}
	for _, r := range discovered {
		segs := strings.Split(strings.Trim(strings.TrimSuffix(r.Path, "{$}"), "/"), "/")
		method := r.Method
		if method == "" {
			method = "ANY"
		}
		templates[shape{method, len(segs)}] = append(templates[shape{method, len(segs)}], segs)
	}

	keys := map[string]bool{}
	for _, e := range authzMatrix {
		segs := strings.Split(strings.Trim(e.Path, "/"), "/")
		for _, method := range []string{e.Method, "ANY"} {
			for _, tmpl := range templates[shape{method, len(segs)}] {
				masked := make([]string, len(segs))
				copy(masked, segs)
				match := true
				for i, ts := range tmpl {
					if strings.HasPrefix(ts, "{") && strings.HasSuffix(ts, "}") {
						masked[i] = "{}"
						continue
					}
					if !strings.EqualFold(ts, segs[i]) {
						match = false
						break
					}
				}
				if match {
					keys[method+" /"+strings.Join(masked, "/")] = true
				}
			}
		}
		// Also index the literal form, for routes with no wildcard.
		m := e.Method
		if m == "" {
			m = "ANY"
		}
		keys[normalizePattern(m, e.Path)] = true
	}
	return keys
}

// TestAuthorizationMatrixCoversEveryRoute fails when the server registers a
// route the matrix does not exercise. This is the guard the issue asks for:
// adding a route without adding a matrix entry breaks the build.
func TestAuthorizationMatrixCoversEveryRoute(t *testing.T) {
	discovered := discoverRegisteredRoutes(t)
	covered := matrixKeys(discovered)

	var missing []discoveredRoute
	for _, r := range discovered {
		key := normalizePattern(r.Method, r.Path)
		if covered[key] {
			continue
		}
		// A route registered without a method is reachable by any method;
		// accept a matrix entry that pins a specific one.
		if r.Method == "" {
			anyMethodCovered := false
			for k := range covered {
				if strings.HasSuffix(k, " "+strings.SplitN(key, " ", 2)[1]) {
					anyMethodCovered = true
					break
				}
			}
			if anyMethodCovered {
				continue
			}
		}
		missing = append(missing, r)
	}

	if len(missing) > 0 {
		t.Errorf("%d registered route(s) are not exercised by authzMatrix.\n"+
			"Add an entry to authzMatrix in authz_matrix_test.go for each:", len(missing))
		for _, r := range missing {
			t.Errorf("  %s %s  (registered at %s)", r.Method, r.Path, r.Source)
		}
	}
}

// TestAuthorizationMatrixHasNoStaleEntries catches the opposite drift: a
// matrix entry for a route the server no longer serves, which would otherwise
// sit there asserting a policy about nothing.
func TestAuthorizationMatrixHasNoStaleEntries(t *testing.T) {
	discovered := discoverRegisteredRoutes(t)

	live := map[string]bool{}
	for _, r := range discovered {
		live[normalizePattern(r.Method, r.Path)] = true
	}

	for _, e := range authzMatrix {
		// Infrastructure routes are registered in main.go, not the handler
		// package, so they are not discoverable here.
		if isInfraPath(e.Path) {
			continue
		}
		matched := false
		for key := range matrixKeys(discovered) {
			if !live[key] {
				continue
			}
			segs := strings.Split(strings.Trim(e.Path, "/"), "/")
			kSegs := strings.Split(strings.Trim(strings.SplitN(key, " ", 2)[1], "/"), "/")
			if len(segs) != len(kSegs) {
				continue
			}
			method := strings.SplitN(key, " ", 2)[0]
			if method != e.Method && method != "ANY" {
				continue
			}
			ok := true
			for i := range segs {
				if kSegs[i] == "{}" {
					continue
				}
				if !strings.EqualFold(kSegs[i], segs[i]) {
					ok = false
					break
				}
			}
			if ok {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("matrix entry %s %s matches no registered route — remove it or fix the path",
				e.Method, e.Path)
		}
	}
}

// isInfraPath reports whether a path is served outside the handler package
// (health, readiness, websocket), which the source scan cannot see.
func isInfraPath(path string) bool {
	for _, p := range []string{"/health", "/healthz", "/readyz", "/ws", "/metrics"} {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}
