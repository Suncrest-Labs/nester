package middleware

import (
	"net/http"
	"strings"
)

// Route costs
//
// The existing limiters count requests. That is the wrong unit: a profile read
// and a Soroban contract invocation that fans out to RPC and price APIs both
// count as one, so a user can sit comfortably inside the request-rate limit
// while saturating every expensive dependency we have. The limiter never
// notices, because it is measuring the wrong thing.
//
// A route's cost is a rough statement of how much downstream work one call
// causes, in units where an ordinary database-backed read is 1. The numbers are
// deliberately coarse — they are a fan-out ordering, not a latency budget — and
// they only need to be right relative to each other.

// DefaultRouteCost is charged for any route with no declared cost. Most
// endpoints are a database read or write and belong here.
const DefaultRouteCost = 1

// Cost tiers. Named so the table below reads as a claim about fan-out rather
// than a list of magic numbers, and so re-tuning a tier is one edit.
const (
	// CostChainWrite is a Soroban contract invocation: build, simulate,
	// sign and submit, then poll for confirmation. Several RPC round-trips
	// against an endpoint we do not own.
	CostChainWrite = 10

	// CostAggregation is multi-protocol APY comparison and TVL rollups:
	// upstream calls to DeFiLlama and price oracles, fanned across
	// protocols and cached with a short TTL.
	CostAggregation = 6

	// CostChainRead is a Soroban simulation with no submission — one RPC
	// round-trip, no ledger write.
	CostChainRead = 4

	// CostSimulation is a projection or scenario run: CPU-bound in-process
	// work over a fetched dataset. Not an external dependency, but not free.
	CostSimulation = 3
)

// RouteCost declares the cost of one method+path-pattern endpoint.
//
// Pattern uses the same wildcard syntax as the Go 1.22 ServeMux patterns the
// routes are registered with ("/api/v1/vaults/{id}/deposit"), so an entry can
// be copied straight from the handler's Register method. A trailing "{name...}"
// matches the rest of the path.
type RouteCost struct {
	Method  string
	Pattern string
	Cost    int
}

// routeCosts is the single declaration of which routes cost more than one unit.
//
// Everything absent from this table costs DefaultRouteCost. Adding an expensive
// endpoint without adding it here is the failure mode this table exists to
// prevent, so keep it next to the handler when you add one.
var routeCosts = []RouteCost{

	// ---- Soroban contract invocations ----
	{http.MethodPost, "/api/v1/vaults/{id}/deposit", CostChainWrite},
	{http.MethodPost, "/api/v1/vaults/{id}/withdraw", CostChainWrite},
	{http.MethodPost, "/api/v1/vaults/{id}/emergency-withdraw", CostChainWrite},
	{http.MethodPost, "/api/v1/vaults/{id}/harvest", CostChainWrite},
	{http.MethodPost, "/api/v1/vaults/{id}/rebalance", CostChainWrite},
	{http.MethodPost, "/api/v1/vault/rebalance", CostChainWrite},
	{http.MethodPost, "/api/v1/admin/vaults/{id}/rebalance", CostChainWrite},

	// ---- Soroban simulations (no submission) ----
	{http.MethodGet, "/api/v1/vaults/{id}/preview-deposit", CostChainRead},
	{http.MethodGet, "/api/v1/vaults/{id}/preview-withdraw", CostChainRead},
	{http.MethodGet, "/api/v1/vaults/{id}/convert", CostChainRead},
	{http.MethodGet, "/api/v1/vaults/{id}/share-price", CostChainRead},
	{http.MethodGet, "/api/v1/vaults/{id}/my-position", CostChainRead},
	{http.MethodGet, "/api/v1/vaults/{id}/harvest/preview", CostChainRead},

	// ---- Multi-protocol APY comparison and TVL aggregation ----
	{http.MethodGet, "/api/v1/yield-opportunities/compare", CostAggregation},
	{http.MethodGet, "/api/v1/yield-opportunities", CostAggregation},
	{http.MethodGet, "/api/v1/yields", CostAggregation},
	{http.MethodGet, "/api/v1/yields/{protocol_slug}/apy-history", CostAggregation},
	{http.MethodGet, "/api/v1/vaults/tvl", CostAggregation},
	{http.MethodGet, "/api/v1/vaults/{id}/tvl", CostAggregation},
	{http.MethodGet, "/api/v1/vaults/{id}/performance/apy", CostAggregation},
	{http.MethodGet, "/api/v1/portfolio/valuation", CostAggregation},

	// ---- In-process simulation over fetched data ----
	{http.MethodPost, "/api/v1/tools/simulation", CostSimulation},
	{http.MethodPost, "/api/v1/tools/projection", CostSimulation},
	{http.MethodGet, "/api/v1/vaults/{id}/projection", CostSimulation},
}

// RouteCosts returns the declared cost table. It is a copy, so callers (tests,
// a future admin endpoint) cannot mutate the live table.
func RouteCosts() []RouteCost {
	out := make([]RouteCost, len(routeCosts))
	copy(out, routeCosts)
	return out
}

// MaxRouteCost is the largest declared cost in the table.
//
// A quota whose capacity is below this makes the route charging it permanently
// unreachable: the bucket can never hold enough tokens to pay for one call, so
// every attempt is rejected with a Retry-After that will not help on the next
// try either. Callers configuring a quota should refuse to start below it
// rather than ship an endpoint that is silently dead.
func MaxRouteCost() int {
	m := DefaultRouteCost
	for _, rc := range routeCosts {
		if rc.Cost > m {
			m = rc.Cost
		}
	}
	return m
}

// CostForRequest returns the declared cost of r, or DefaultRouteCost when no
// entry matches.
//
// Where several patterns match, the most specific wins — "/api/v1/vaults/tvl"
// beats "/api/v1/vaults/{id}/tvl" for the aggregate endpoint — so a literal
// route is never shadowed by a wildcard one declared earlier in the table.
func CostForRequest(r *http.Request) int {
	cost, _ := costAndLabel(r)
	return cost
}

// costAndLabel resolves the cost and the matched pattern in one pass over the
// table. The label is for metrics: patterns are a bounded set, unlike request
// paths, which carry IDs and would give a metric unbounded cardinality.
// Requests matching nothing report their method alone.
func costAndLabel(r *http.Request) (cost int, label string) {
	cost = DefaultRouteCost
	label = r.Method + " *"

	best := -1
	for _, rc := range routeCosts {
		if rc.Method != r.Method {
			continue
		}
		score, ok := matchPattern(rc.Pattern, r.URL.Path)
		if !ok || score <= best {
			continue
		}
		best = score
		cost = rc.Cost
		label = rc.Method + " " + rc.Pattern
	}
	return cost, label
}

// matchPattern reports whether path satisfies a ServeMux-style pattern, and how
// specific the match is: the number of literal (non-wildcard) segments matched.
// A pattern of all literals therefore outranks one with a wildcard in the same
// position, which is how the tvl routes above disambiguate.
func matchPattern(pattern, path string) (specificity int, ok bool) {
	pSegs := splitPath(pattern)
	uSegs := splitPath(path)

	for i, seg := range pSegs {
		// "{name...}" matches this segment and everything after it.
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "...}") {
			return specificity, len(uSegs) >= i
		}
		if i >= len(uSegs) {
			return 0, false
		}
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			continue // single-segment wildcard, adds no specificity
		}
		if seg != uSegs[i] {
			return 0, false
		}
		specificity++
	}

	if len(uSegs) != len(pSegs) {
		return 0, false
	}
	return specificity, true
}

// splitPath lives in ratelimit_routes.go; the route matcher there and the cost
// table here segment paths the same way.
