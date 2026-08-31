package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/freshness"
)

// Freshness headers describing how current the indexed data behind a response
// is (nester#1088, consumed by the UI degradation work in nester#35).
//
// Balances served from the event indexer are only as current as the indexer
// is. When it falls behind, the API keeps answering — a stale balance is far
// more useful than an error, and a 5xx would take down screens that do not
// depend on indexed data at all — but the client is told, so it can say
// "as of a few minutes ago" instead of implying the number is live.
//
// Headers rather than a body field, deliberately: the freshness of the indexed
// view is a property of the whole response, not of one payload, and every
// route that serves indexed data gets it automatically. A per-handler envelope
// field would be opt-in, and the handler added next year that forgot to opt in
// would silently serve stale balances as fresh — which is the failure this
// issue exists to remove.
const (
	// HeaderIndexerStale is the authoritative fresh/stale answer, "true" or
	// "false". A client that reads nothing else can read this.
	HeaderIndexerStale = "X-Indexer-Stale"

	// HeaderIndexerLagSeconds is how far behind the chain the indexed view is,
	// in whole seconds, rounded up so it never understates staleness. This is
	// the figure a UI renders.
	HeaderIndexerLagSeconds = "X-Indexer-Lag-Seconds"

	// HeaderIndexerLagLedgers is the same lag in ledgers. Omitted entirely
	// when the indexer has not yet reported a position: no header means "not
	// known", which a zero would misreport as "exactly at the tip".
	HeaderIndexerLagLedgers = "X-Indexer-Lag-Ledgers"

	// HeaderIndexerStalenessBudgetSeconds is the budget the stale flag was
	// decided against, so a client can present the threshold without
	// hardcoding it.
	HeaderIndexerStalenessBudgetSeconds = "X-Indexer-Staleness-Budget-Seconds"
)

// freshnessHeaders is the list browsers must be allowed to read cross-origin.
// See CORS, which sets Access-Control-Expose-Headers from it.
var freshnessHeaders = []string{
	HeaderIndexerStale,
	HeaderIndexerLagSeconds,
	HeaderIndexerLagLedgers,
	HeaderIndexerStalenessBudgetSeconds,
}

// apiPathPrefix scopes the freshness headers to the versioned API. Liveness
// probes and any non-API route are left untouched: freshness describes the
// data an API response carries, and /health answers a different question.
const apiPathPrefix = "/api/"

// IndexerFreshness returns a middleware that annotates every API response with
// the freshness of the indexed data behind it.
//
// A nil reader disables annotation and passes requests straight through, so a
// deployment without an indexer behaves exactly as it did before.
func IndexerFreshness(reader freshness.Reader) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if reader == nil {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, apiPathPrefix) {
				next.ServeHTTP(w, r)
				return
			}

			// Written before the handler runs, because headers are only
			// mutable until the first WriteHeader and a handler may write
			// immediately.
			snapshot := reader.Snapshot()
			header := w.Header()
			header.Set(HeaderIndexerStale, strconv.FormatBool(snapshot.Stale))
			header.Set(HeaderIndexerLagSeconds, strconv.FormatInt(wholeSecondsUp(snapshot.Lag), 10))
			header.Set(HeaderIndexerStalenessBudgetSeconds, strconv.FormatInt(wholeSecondsUp(snapshot.Budget), 10))
			if snapshot.Sampled {
				header.Set(HeaderIndexerLagLedgers, strconv.FormatUint(snapshot.LagLedgers, 10))
			}

			next.ServeHTTP(w, r)
		})
	}
}

// wholeSecondsUp rounds a duration up to whole seconds.
//
// Up rather than down so the reported lag is never smaller than the real one:
// a client comparing the number against the budget must not conclude the data
// is inside it when the stale flag says otherwise.
func wholeSecondsUp(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64((d + time.Second - 1) / time.Second)
}
