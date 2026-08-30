package listquery_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestNoNewUnboundedListLimits is the guard nester#1225 asks for: "a test or
// lint that fails when a new list endpoint ships without the [pagination]
// contract". It scans every non-test .go file under internal/ for a struct
// literal field named PerPage or Limit set to an int literal above
// listquery.MaxPerPage's order of magnitude (>= 500), and fails the build if
// one appears that is not in knownLargeLimits below.
//
// This is deliberately an ALLOWLIST, not a bare threshold check: every
// existing large limit in this codebase is a considered, reviewed decision
// (see each entry's comment) rather than an accident, and a bare "no literal
// over N" rule would either need to re-litigate those on every test run or
// produce false positives that train people to ignore the test. A NEW large
// limit must be added here explicitly, with the same kind of justification
// the existing entries carry — that's what "ships without the contract"
// means failing loudly, without making already-reviewed code re-fail forever.
//
// listquery.MaxPerPage (100) is the bound for anything reachable from an
// HTTP list endpoint via ParsePage — those already fail closed (ParsePage
// rejects per_page > MaxPerPage). This test covers the other half of the
// issue's evidence: internal, non-HTTP call sites that construct a
// *ListFilter directly with a hardcoded large PerPage/Limit to simulate
// "fetch everything" rather than either paging through the real total or
// being genuinely bounded by something other than transaction volume.
func TestNoNewUnboundedListLimits(t *testing.T) {
	// knownLargeLimits are file:line -> why. Each entry here must be a
	// call site actually reviewed and found safe (bounded by something
	// other than unbounded transaction/record volume — typically "how many
	// vaults a single user can create through the product UI", which the
	// referenced issues discuss), or already fixed to page through the real
	// total (comparators.go, adapters.go's TxPendingSource) and therefore
	// no longer present in this list at all.
	knownLargeLimits := map[string]string{
		"internal/service/portfolio_service.go:38":    "nester#1193/#1225: ListUserVaults, bounded by a single user's vault count (product-UI-created, not transaction volume).",
		"internal/service/performance/service.go:391": "nester#1193/#1225: ListUserVaults, same per-user bound as portfolio_service.go.",
		"internal/service/user_vaults_service.go:45":  "nester#1225: ListUserVaults, same per-user bound, additionally scoped to StatusActive.",
		"internal/valuation/adapters.go:50":           "nester#1193/#1225: VaultPositionSource.Positions — ListUserVaults, same per-user bound (see the function's own doc comment).",
	}

	root := moduleRoot(t)
	internalDir := filepath.Join(root, "internal")

	fset := token.NewFileSet()
	violations := []string{}
	matchedKnown := map[string]bool{}

	err := filepath.WalkDir(internalDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		file, parseErr := parser.ParseFile(fset, path, src, 0)
		if parseErr != nil {
			// A file that doesn't parse is a compile error elsewhere, not
			// this test's job to report — skip rather than fail here.
			return nil
		}

		// ToSlash so the allowlist keys below (written with forward slashes)
		// match on Windows too, where filepath.Rel returns backslashes and
		// every entry would otherwise miss and report as a fresh violation.
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)

		ast.Inspect(file, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || (key.Name != "PerPage" && key.Name != "Limit") {
				return true
			}
			lit, ok := kv.Value.(*ast.BasicLit)
			if !ok || lit.Kind != token.INT {
				return true
			}
			// ParseInt with base 0, not Atoi: a token.INT literal may be
			// written 1_000, 0x2710 or 0o1750, and Atoi rejects all three.
			// Because the error branch skips rather than reports, those
			// forms would slip past the guard entirely.
			value, convErr := strconv.ParseInt(lit.Value, 0, 64)
			if convErr != nil || value < 500 {
				return true
			}

			pos := fset.Position(kv.Pos())
			key2 := rel + ":" + strconv.Itoa(pos.Line)
			if _, known := knownLargeLimits[key2]; !known {
				violations = append(violations, key2+": "+key.Name+": "+lit.Value)
			} else {
				matchedKnown[key2] = true
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Errorf(
			"found %d list-limit literal(s) >= 500 not in the reviewed allowlist "+
				"(pkg/listquery/pagination_contract_test.go's knownLargeLimits):\n  %s\n\n"+
				"A list endpoint or internal query must either (a) use listquery.ParsePage "+
				"(bounded by MaxPerPage=100) if it's reachable from an HTTP request, "+
				"(b) page through the real total rather than substituting a large fixed "+
				"limit (see reconciliation/comparators.go's vaultReconcilePageSize or "+
				"valuation/adapters.go's TxPendingSource.PendingDeposits for the pattern), "+
				"or (c) if genuinely bounded by something other than record-volume growth "+
				"(e.g. a single user's own vault count), add it to knownLargeLimits with "+
				"the same kind of justification the existing entries carry.",
			len(violations), strings.Join(violations, "\n  "),
		)
	}

	// Sanity check on the test itself. Checking only that the map is non-empty
	// does not catch staleness: it is a map literal, so it is non-empty by
	// construction even when every entry has drifted off its call site.
	// Assert each entry still matches a real one instead.
	//
	// This is also what makes the file:line keying self-correcting. Inserting
	// lines above an allowlisted literal moves it, and the guard then reports
	// the moved literal as a violation AND its old key as stale — naming both
	// halves, so the fix is to update the key rather than to guess at it. A
	// content-keyed scheme would avoid that churn, but every candidate key
	// (enclosing function, literal value) is ambiguous here: several entries
	// are the same ListUserVaults call with the same value, so the key would
	// stop identifying a unique site.
	if len(knownLargeLimits) == 0 {
		t.Fatal("knownLargeLimits is empty — this test would no longer verify anything")
	}
	stale := []string{}
	for key := range knownLargeLimits {
		if !matchedKnown[key] {
			stale = append(stale, key)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf(
			"%d knownLargeLimits entr(ies) no longer match a large list-limit literal:\n  %s\n\n"+
				"The referenced code moved, changed, or was fixed. Update the allowlist to the "+
				"current file:line (or drop the entry if the limit is gone) so this guard keeps "+
				"covering what it claims to.",
			len(stale), strings.Join(stale, "\n  "),
		)
	}
}

// moduleRoot finds the apps/api module root by walking up from this test
// file's own directory to the nearest go.mod — robust to the test being run
// from any working directory (go test ./..., an IDE, CI).
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod walking up from pkg/listquery")
		}
		dir = parent
	}
}
