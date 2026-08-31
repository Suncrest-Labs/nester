package handler

import (
	"errors"
	"math"
	"net/http"
	"strconv"

	"github.com/suncrestlabs/nester/apps/api/internal/breaker"
	"github.com/suncrestlabs/nester/apps/api/internal/retry"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// writeUpstreamUnavailable answers a request that failed because a chain
// upstream was unreachable: the circuit breaker was open, or the retry helper
// used up its attempts.
//
// 503 with Retry-After rather than 500, because the request is retriable and
// nothing is wrong with it — a 500 tells the client the request was at fault
// and tells the operator to look for a bug in the API.
//
// Deliberately not logged. An open breaker can reject every request for its
// whole open period, and a log line each would turn an upstream outage into a
// logging outage; the breaker's rejection counter and the RPC exhaustion
// counter carry that volume instead.
func writeUpstreamUnavailable(w http.ResponseWriter, err error) {
	w.Header().Set("Retry-After", upstreamRetryAfterSeconds(err))
	response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(
		http.StatusServiceUnavailable,
		"UPSTREAM_UNAVAILABLE",
		"the Stellar network is temporarily unreachable; please retry shortly",
	))
}

// isUpstreamUnavailable reports whether err is a chain-boundary rejection that
// should be surfaced as 503 rather than 500.
func isUpstreamUnavailable(err error) bool {
	return errors.Is(err, breaker.ErrOpen) || errors.Is(err, retry.ErrExhausted)
}

// upstreamRetryAfterSeconds derives a Retry-After value from the breaker's own
// cooldown when it has one, so a client that honours the header comes back
// roughly when the breaker is ready to probe rather than hammering it while
// still open.
func upstreamRetryAfterSeconds(err error) string {
	var openErr *breaker.OpenError
	if errors.As(err, &openErr) {
		if seconds := int(math.Ceil(openErr.RetryIn.Seconds())); seconds > 0 {
			return strconv.Itoa(seconds)
		}
	}
	return "1"
}
