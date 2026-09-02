package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// expensiveRoutes lists, independently of the cost table itself, the endpoints
// the issue identifies as expensive: the intelligence relay, the Soroban chain
// invoker, TVL aggregation, and multi-protocol APY comparison.
//
// Declaring them here rather than deriving them from routeCosts is the point:
// if someone removes an entry from the table, this test fails instead of
// silently agreeing with the removal.
var expensiveRoutes = []struct {
	method string
	path   string
	what   string
}{
	// Intelligence relay and model calls.
	{http.MethodPost, "/api/v1/intelligence/chat", "intelligence relay"},
	{http.MethodPost, "/api/v1/intelligence/analyze", "model analysis"},
	{http.MethodPost, "/api/v1/intelligence/coaching", "model coaching"},
	{http.MethodPost, "/api/v1/intelligence/savings-plan", "model savings plan"},
	{http.MethodPost, "/api/v1/intelligence/recommend/vault", "model vault recommendation"},
	{http.MethodGet, "/api/v1/intelligence/market", "market intelligence"},
	{http.MethodGet, "/api/v1/intelligence/portfolio/abc-123", "portfolio intelligence"},
	{http.MethodGet, "/api/v1/portfolio/abc-123/insights", "portfolio insights"},
	{http.MethodGet, "/api/v1/vaults/v-1/recommendations", "vault recommendations"},
	{http.MethodPost, "/api/v1/vaults/v-1/rebalance/suggest", "rebalance suggestion"},

	// Soroban chain invoker.
	{http.MethodPost, "/api/v1/vaults/v-1/deposit", "chain deposit"},
	{http.MethodPost, "/api/v1/vaults/v-1/withdraw", "chain withdraw"},
	{http.MethodPost, "/api/v1/vaults/v-1/emergency-withdraw", "chain emergency withdraw"},
	{http.MethodPost, "/api/v1/vaults/v-1/harvest", "chain harvest"},
	{http.MethodPost, "/api/v1/vaults/v-1/rebalance/execute", "chain rebalance execute"},
	{http.MethodGet, "/api/v1/vaults/v-1/preview-deposit", "chain simulation"},
	{http.MethodGet, "/api/v1/vaults/v-1/share-price", "chain share price"},

	// TVL aggregation.
	{http.MethodGet, "/api/v1/vaults/tvl", "TVL aggregate"},
	{http.MethodGet, "/api/v1/vaults/v-1/tvl", "per-vault TVL"},

	// Multi-protocol APY comparison.
	{http.MethodGet, "/api/v1/yield-opportunities/compare", "multi-protocol APY comparison"},
	{http.MethodGet, "/api/v1/yield-opportunities", "yield opportunities"},
	{http.MethodGet, "/api/v1/yields", "yields"},
	{http.MethodGet, "/api/v1/yields/blend/apy-history", "protocol APY history"},
	{http.MethodGet, "/api/v1/vaults/v-1/performance/apy", "vault APY"},
}

func costOf(t *testing.T, method, path string) int {
	t.Helper()
	return CostForRequest(httptest.NewRequest(method, path, nil))
}

// Every endpoint the issue calls expensive must carry a weight above 1.
// Counting them as one request is exactly the bug: a caller stays inside the
// request-rate limit while saturating Anthropic, DeFiLlama and Soroban RPC.
func TestExpensiveRoutesCostMoreThanOne(t *testing.T) {
	for _, rt := range expensiveRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			got := costOf(t, rt.method, rt.path)
			if got <= DefaultRouteCost {
				t.Errorf("%s (%s) costs %d, want > %d — an expensive route counted as an ordinary one",
					rt.path, rt.what, got, DefaultRouteCost)
			}
		})
	}
}

// The relay is the most expensive thing the API does: a model call plus
// model-driven tool fan-out. Nothing should outrank it, or the ordering that
// makes the weights meaningful is wrong.
func TestRelayIsTheMostExpensiveRoute(t *testing.T) {
	relay := costOf(t, http.MethodPost, "/api/v1/intelligence/chat")
	for _, rc := range RouteCosts() {
		if rc.Cost > relay {
			t.Errorf("%s %s costs %d, above the relay's %d", rc.Method, rc.Pattern, rc.Cost, relay)
		}
	}
}

// Ordinary reads and writes must stay at the default, or the quota stops being
// a statement about expensive work and becomes a second request limiter.
func TestOrdinaryRoutesCostTheDefault(t *testing.T) {
	ordinary := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/users/profile"},
		{http.MethodGet, "/api/v1/vaults"},
		{http.MethodGet, "/api/v1/vaults/v-1"},
		{http.MethodGet, "/api/v1/transactions"},
		{http.MethodPost, "/api/v1/users/watchlist"},
		{http.MethodPatch, "/api/v1/users/profile"},
		{http.MethodGet, "/api/v1/activity"},
		{http.MethodGet, "/health"},
	}
	for _, rt := range ordinary {
		if got := costOf(t, rt.method, rt.path); got != DefaultRouteCost {
			t.Errorf("%s %s costs %d, want the default %d", rt.method, rt.path, got, DefaultRouteCost)
		}
	}
}

// Every declared cost must be positive and above the default — an entry at or
// below 1 is either a mistake or dead weight in the table.
func TestDeclaredCostsAreAboveDefault(t *testing.T) {
	if len(RouteCosts()) == 0 {
		t.Fatal("cost table is empty")
	}
	for _, rc := range RouteCosts() {
		if rc.Cost <= DefaultRouteCost {
			t.Errorf("%s %s declares cost %d; entries must exceed the default %d or be removed",
				rc.Method, rc.Pattern, rc.Cost, DefaultRouteCost)
		}
	}
}

// One route must not be declared twice: duplicates make the effective cost
// depend on table order, which nobody reading the table would expect.
func TestNoDuplicateRouteDeclarations(t *testing.T) {
	seen := make(map[string]int)
	for _, rc := range RouteCosts() {
		k := rc.Method + " " + rc.Pattern
		if prev, dup := seen[k]; dup {
			t.Errorf("%s declared twice (costs %d and %d)", k, prev, rc.Cost)
		}
		seen[k] = rc.Cost
	}
}

// RouteCosts hands out a copy; mutating it must not affect what the middleware
// charges.
func TestRouteCostsReturnsACopy(t *testing.T) {
	before := costOf(t, http.MethodPost, "/api/v1/intelligence/chat")

	table := RouteCosts()
	for i := range table {
		table[i].Cost = 999
	}

	if after := costOf(t, http.MethodPost, "/api/v1/intelligence/chat"); after != before {
		t.Errorf("mutating the returned table changed the live cost: %d -> %d", before, after)
	}
}

// A literal segment must beat a wildcard in the same position. /vaults/tvl is
// the aggregate rollup; /vaults/{id}/tvl is one vault. Both are declared, and
// matching the wrong one would charge the wrong weight.
func TestLiteralRoutesBeatWildcards(t *testing.T) {
	aggregate := costOf(t, http.MethodGet, "/api/v1/vaults/tvl")
	perVault := costOf(t, http.MethodGet, "/api/v1/vaults/v-1/tvl")

	if aggregate != CostAggregation {
		t.Errorf("GET /api/v1/vaults/tvl = %d, want %d", aggregate, CostAggregation)
	}
	if perVault != CostAggregation {
		t.Errorf("GET /api/v1/vaults/v-1/tvl = %d, want %d", perVault, CostAggregation)
	}
}

// The method is part of the match: GET and POST on the same path are different
// endpoints with different fan-out.
func TestCostIsMethodSpecific(t *testing.T) {
	get := costOf(t, http.MethodGet, "/api/v1/intelligence/recommend/vault")
	post := costOf(t, http.MethodPost, "/api/v1/intelligence/recommend/vault")

	if get != CostIntelligenceRead {
		t.Errorf("GET recommend/vault = %d, want %d", get, CostIntelligenceRead)
	}
	if post != CostLLMAnalysis {
		t.Errorf("POST recommend/vault = %d, want %d", post, CostLLMAnalysis)
	}

	// A declared POST route called with DELETE is not that route.
	if got := costOf(t, http.MethodDelete, "/api/v1/intelligence/chat"); got != DefaultRouteCost {
		t.Errorf("DELETE on a POST-only route = %d, want the default %d", got, DefaultRouteCost)
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"/api/v1/vaults/{id}/deposit", "/api/v1/vaults/abc/deposit", true},
		{"/api/v1/vaults/{id}/deposit", "/api/v1/vaults/abc/withdraw", false},
		{"/api/v1/vaults/{id}/deposit", "/api/v1/vaults/deposit", false},
		{"/api/v1/vaults/{id}/deposit", "/api/v1/vaults/a/b/deposit", false},
		{"/api/v1/vaults/tvl", "/api/v1/vaults/tvl", true},
		{"/api/v1/vaults/tvl", "/api/v1/vaults/tvl/extra", false},
		{"/api/v1/yields", "/api/v1/yields", true},
		{"/api/v1/yields", "/api/v1/yields/blend", false},
		// A wildcard must not match an empty segment.
		{"/api/v1/vaults/{id}", "/api/v1/vaults/", false},
	}
	for _, tt := range tests {
		if _, ok := matchPattern(tt.pattern, tt.path); ok != tt.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.pattern, tt.path, ok, tt.want)
		}
	}
}

// A trailing slash must not turn a declared route into an undeclared one and
// quietly drop its cost to 1.
func TestTrailingSlashDoesNotEscapeTheCost(t *testing.T) {
	if got := costOf(t, http.MethodGet, "/api/v1/yields/"); got != CostAggregation {
		t.Errorf("GET /api/v1/yields/ = %d, want %d", got, CostAggregation)
	}
}

// A quota smaller than the priciest route makes that route permanently
// unreachable, so the two numbers have to be checked against each other
// somewhere. MaxRouteCost is what makes that check possible.
func TestMaxRouteCostMatchesTheTable(t *testing.T) {
	want := DefaultRouteCost
	for _, rc := range RouteCosts() {
		if rc.Cost > want {
			want = rc.Cost
		}
	}
	if got := MaxRouteCost(); got != want {
		t.Errorf("MaxRouteCost() = %d, want %d", got, want)
	}
	if MaxRouteCost() != CostLLMRelay {
		t.Errorf("MaxRouteCost() = %d, want the relay cost %d", MaxRouteCost(), CostLLMRelay)
	}
}
