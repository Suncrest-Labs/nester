package middleware

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cost-weighted quotas
//
// This sits alongside the existing request-count limiters rather than replacing
// them: they still bound request *rate* per IP, while this bounds downstream
// *work* per user. A caller has to stay inside both.
//
// The accounting is a token bucket rather than a fixed window. A fixed window
// lets a caller spend its whole allowance in the last instant of one window and
// again in the first instant of the next, so the effective burst is twice the
// configured limit at exactly the moment the expensive dependencies are already
// under load. A bucket refills continuously and has no such boundary.

// quotaBypassHeader lets a load test opt out of quota accounting on a
// per-request basis. Inert unless a bypass token is configured; see
// QuotaConfig.BypassToken.
const quotaBypassHeader = "X-RateLimit-Bypass" // #nosec G101 -- the name of a header, not a credential; the token it carries is compared against QuotaConfig.BypassToken

// quotaKeyTTLFactor is how many windows an idle bucket survives in Redis before
// expiring. An expired bucket is indistinguishable from a full one, which is
// the correct reading: the caller has not spent anything in that time.
const quotaKeyTTLFactor = 2

// QuotaDecision is the outcome of one accounting call.
type QuotaDecision struct {
	Allowed bool
	// Limit is the bucket capacity, in cost units.
	Limit int
	// Remaining is the balance after this request was charged.
	Remaining int
	// RetryAfter is how long until this request's cost would be affordable.
	// Zero when the request was allowed.
	RetryAfter time.Duration
	// Reset is how long until the bucket is full again.
	Reset time.Duration
	// Degraded is set when the backing store could not be reached and the
	// request was allowed without being counted. Nothing about Remaining or
	// Reset is meaningful in that case.
	Degraded bool
}

// QuotaLimiter accounts cost units against a key.
type QuotaLimiter interface {
	AllowN(ctx context.Context, key string, cost int) QuotaDecision
}

// QuotaEvent describes one accounting decision, for metrics (#1043). Emitting
// through a callback keeps this package free of a metrics dependency.
type QuotaEvent struct {
	// Key is the quota subject: "u:{userID}" or "ip:{addr}".
	Key string
	// Route is the matched cost-table pattern, or the request path when the
	// route was charged the default cost. Safe to use as a metric label:
	// patterns are bounded by the table, unlike raw paths.
	Route    string
	Cost     int
	Allowed  bool
	Degraded bool
}

// QuotaObserver receives every accounting decision. Optional.
type QuotaObserver func(QuotaEvent)

// QuotaConfig configures the middleware.
//
// The limit and window live on the QuotaLimiter, not here: the decision it
// returns already carries the limit it enforced, and holding the number in two
// places is how the headers end up advertising a budget nobody enforces.
type QuotaConfig struct {
	// Enabled turns accounting off entirely. A disabled quota still emits no
	// headers and charges nothing — the documented way to run a load test
	// against an environment without re-tuning every limit.
	Enabled bool
	// BypassToken, when non-empty, lets a request carrying it in the
	// X-RateLimit-Bypass header skip accounting. Empty (the default)
	// disables the mechanism completely, so an unset config cannot be
	// bypassed by guessing.
	BypassToken string
	// ExcludePrefixes are path prefixes that skip accounting entirely —
	// liveness, readiness and metrics, which an orchestrator must be able to
	// reach under any load. Without this they would be charged against the
	// quota of whatever IP the orchestrator happens to probe from.
	ExcludePrefixes []string
	// Logger receives fail-open warnings. Defaults to slog.Default().
	Logger *slog.Logger
	// Observer receives every decision, for metrics. Optional.
	Observer QuotaObserver
}

// NewQuotaLimiter returns a Redis-backed limiter when rc is non-nil and a
// process-local one otherwise, mirroring NewLimiter's dual-mode behaviour.
//
// The in-memory fallback bounds a single instance only. That is the same
// trade-off the existing limiters make without Redis, and it is still strictly
// better than not counting cost at all.
func NewQuotaLimiter(rc *redis.Client, prefix string, limit int, window time.Duration, logger *slog.Logger) QuotaLimiter {
	if logger == nil {
		logger = slog.Default()
	}
	if rc != nil {
		return &redisQuotaLimiter{rc: rc, prefix: prefix, limit: limit, window: window, logger: logger}
	}
	return newMemoryQuotaLimiter(limit, window)
}

// ---------------------------------------------------------------------------
// Redis token bucket
// ---------------------------------------------------------------------------

// quotaScript is a token bucket held in a Redis hash: "t" is the balance and
// "ts" the millisecond timestamp it was last refilled at. Refill is computed
// lazily on read, so an idle key costs nothing and there is no sweeper.
//
// The whole read-refill-charge-write cycle runs as one script so two instances
// charging the same user concurrently cannot both see the pre-charge balance.
//
// `now` is supplied by the caller rather than read from Redis TIME: TIME is
// non-deterministic and forces effect replication, and passing it in keeps the
// script usable against a replica for tests. The cost is a sensitivity to clock
// skew between API instances — a skewed instance grants or withholds a little
// refill, bounded by the skew itself, which is acceptable for a quota.
var quotaScript = redis.NewScript(`
local capacity    = tonumber(ARGV[1])
local refillPerMs = tonumber(ARGV[2])
local now         = tonumber(ARGV[3])
local cost        = tonumber(ARGV[4])

local state  = redis.call("HMGET", KEYS[1], "t", "ts")
local tokens = tonumber(state[1])
local ts     = tonumber(state[2])
if tokens == nil or ts == nil then
	tokens = capacity
	ts = now
end

local elapsed = now - ts
if elapsed < 0 then
	-- Clock went backwards (skew, NTP step). Refill nothing rather than
	-- draining the bucket by a negative amount.
	elapsed = 0
end
tokens = math.min(capacity, tokens + elapsed * refillPerMs)

local allowed = 0
if tokens >= cost then
	tokens = tokens - cost
	allowed = 1
end

-- tostring, not the bare number: how Redis coerces a Lua float argument has
-- varied across versions, and a balance silently truncated to an integer would
-- lose every partial refill.
-- Never move the refill base backwards: a clock that steps back and then
-- forward again would otherwise be paid refill for the same interval twice.
local base = now
if ts > now then
	base = ts
end
redis.call("HSET", KEYS[1], "t", tostring(tokens), "ts", tostring(base))
redis.call("PEXPIRE", KEYS[1], ARGV[5])

-- Clamped so a pathologically small refill rate cannot return an out-of-range
-- number that the integer conversion below would mangle. The clamp is far
-- longer than any sane window, so it never truncates a real answer.
local maxMs = 86400000
local retry = 0
if allowed == 0 then
	retry = math.min(maxMs, math.ceil((cost - tokens) / refillPerMs))
end
local reset = math.min(maxMs, math.ceil((capacity - tokens) / refillPerMs))

return {allowed, math.floor(tokens), retry, reset}
`)

type redisQuotaLimiter struct {
	rc     *redis.Client
	prefix string
	limit  int
	window time.Duration
	logger *slog.Logger
}

func (l *redisQuotaLimiter) AllowN(ctx context.Context, key string, cost int) QuotaDecision {
	refillPerMs := refillRate(l.limit, l.window)
	ttl := l.window * quotaKeyTTLFactor

	callCtx, cancel := context.WithTimeout(ctx, redisCallTimeout)
	defer cancel()

	res, err := quotaScript.Run(callCtx, l.rc,
		[]string{"rlq:" + l.prefix + ":" + key},
		l.limit,
		refillPerMs,
		time.Now().UnixMilli(),
		cost,
		ttl.Milliseconds(),
	).Result()
	if err != nil {
		// Fail open. A limiter outage must not become a service outage: the
		// worst case for passing traffic through is a dependency bill, and
		// the worst case for rejecting it is a dead API.
		l.logger.WarnContext(ctx, "quota limiter: redis error, failing open",
			"prefix", l.prefix, "error", err)
		return QuotaDecision{Allowed: true, Limit: l.limit, Degraded: true}
	}

	vals, ok := res.([]any)
	if !ok || len(vals) != 4 {
		l.logger.WarnContext(ctx, "quota limiter: unexpected redis result, failing open",
			"prefix", l.prefix)
		return QuotaDecision{Allowed: true, Limit: l.limit, Degraded: true}
	}

	allowed, _ := vals[0].(int64)
	remaining, _ := vals[1].(int64)
	retryMS, _ := vals[2].(int64)
	resetMS, _ := vals[3].(int64)

	return QuotaDecision{
		Allowed:    allowed == 1,
		Limit:      l.limit,
		Remaining:  int(remaining),
		RetryAfter: time.Duration(retryMS) * time.Millisecond,
		Reset:      time.Duration(resetMS) * time.Millisecond,
	}
}

// ---------------------------------------------------------------------------
// In-memory token bucket
// ---------------------------------------------------------------------------

type memoryQuotaBucket struct {
	tokens float64
	last   time.Time
}

type memoryQuotaLimiter struct {
	mu      sync.Mutex
	buckets map[string]*memoryQuotaBucket
	limit   int
	window  time.Duration
	// lastSweep is when idle buckets were last evicted. Without this the map
	// grows one entry per distinct user or IP and never shrinks, which on a
	// long-lived process is a slow leak keyed by traffic diversity.
	lastSweep time.Time
	// now is injectable so refill behaviour can be asserted at exact
	// instants instead of by sleeping, which is what makes the
	// window-boundary test deterministic rather than merely likely.
	now func() time.Time
}

func newMemoryQuotaLimiter(limit int, window time.Duration) *memoryQuotaLimiter {
	return &memoryQuotaLimiter{
		buckets: make(map[string]*memoryQuotaBucket),
		limit:   limit,
		window:  window,
		now:     time.Now,
	}
}

// sweepLocked drops buckets that have refilled to capacity and gone quiet. A
// full bucket carries no state a fresh one would not reproduce, so forgetting
// it is free. Runs at most once per window; callers must hold l.mu.
func (l *memoryQuotaLimiter) sweepLocked(now time.Time, refillPerMs float64) {
	if now.Sub(l.lastSweep) < l.window {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		elapsed := float64(now.Sub(b.last).Milliseconds())
		if elapsed < 0 {
			continue
		}
		if b.tokens+elapsed*refillPerMs >= float64(l.limit) {
			delete(l.buckets, k)
		}
	}
}

func (l *memoryQuotaLimiter) AllowN(_ context.Context, key string, cost int) QuotaDecision {
	refillPerMs := refillRate(l.limit, l.window)
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.lastSweep.IsZero() {
		l.lastSweep = now
	}
	l.sweepLocked(now, refillPerMs)

	b, ok := l.buckets[key]
	if !ok {
		b = &memoryQuotaBucket{tokens: float64(l.limit), last: now}
		l.buckets[key] = b
	}

	if elapsed := now.Sub(b.last).Milliseconds(); elapsed > 0 {
		b.tokens = math.Min(float64(l.limit), b.tokens+float64(elapsed)*refillPerMs)
		b.last = now
	}
	// b.last is only ever moved forward. A clock that steps back and then
	// forward again would otherwise be paid refill for the same interval twice.

	allowed := b.tokens >= float64(cost)
	if allowed {
		b.tokens -= float64(cost)
	}

	d := QuotaDecision{
		Allowed:   allowed,
		Limit:     l.limit,
		Remaining: int(b.tokens),
		Reset:     msUntil(float64(l.limit)-b.tokens, refillPerMs),
	}
	if !allowed {
		d.RetryAfter = msUntil(float64(cost)-b.tokens, refillPerMs)
	}
	return d
}

// maxQuotaWait bounds the reported wait. It mirrors the clamp in the Lua
// script and, more importantly, keeps a pathologically small refill rate from
// producing an infinity — converting one to a time.Duration is undefined and
// would surface as a nonsense Retry-After rather than a clean error.
const maxQuotaWait = 24 * time.Hour

// msUntil is how long `deficit` tokens take to accrue at refillPerMs.
func msUntil(deficit, refillPerMs float64) time.Duration {
	if deficit <= 0 {
		return 0
	}
	ms := math.Ceil(deficit / refillPerMs)
	if math.IsInf(ms, 0) || math.IsNaN(ms) || ms > float64(maxQuotaWait.Milliseconds()) {
		return maxQuotaWait
	}
	return time.Duration(ms) * time.Millisecond
}

// refillRate is tokens restored per millisecond. Guarded against a zero rate,
// which would make a bucket that never refills and divisions that return
// infinity.
func refillRate(limit int, window time.Duration) float64 {
	ms := float64(window.Milliseconds())
	if ms <= 0 || limit <= 0 {
		return math.SmallestNonzeroFloat64
	}
	return float64(limit) / ms
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

// CostQuota returns middleware that charges each request its declared route
// cost against the caller's quota.
//
// It must be installed after the authentication middleware so authenticated
// requests are keyed by user; anything still anonymous at this point falls back
// to per-IP accounting, which preserves the existing behaviour for public
// routes.
//
// RateLimit-Limit / -Remaining / -Reset are set on every accounted response,
// not only rejections, so a well-behaved client can slow down before it is
// pushed. They are omitted when accounting was skipped or degraded, because any
// number reported then would be invented.
func CostQuota(l QuotaLimiter, cfg QuotaConfig) func(http.Handler) http.Handler {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled || excluded(r.URL.Path, cfg.ExcludePrefixes) || quotaBypassed(r, cfg.BypassToken) {
				next.ServeHTTP(w, r)
				return
			}

			cost, route := costAndLabel(r)
			key := quotaKey(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			d := l.AllowN(r.Context(), key, cost)

			if cfg.Observer != nil {
				cfg.Observer(QuotaEvent{
					Key:      key,
					Route:    route,
					Cost:     cost,
					Allowed:  d.Allowed,
					Degraded: d.Degraded,
				})
			}

			if d.Degraded {
				// Counted nothing, so claim nothing.
				next.ServeHTTP(w, r)
				return
			}

			setQuotaHeaders(w, d)

			if !d.Allowed {
				// Debug, not Warn: the volume here is chosen by whoever is
				// being throttled, so warning on every rejection hands an
				// abusive caller a log-amplification lever. Rejections are
				// meant to be visible as a metric via Observer (#1043); this
				// line exists for investigating one specific caller.
				logger.DebugContext(r.Context(), "quota exhausted",
					"key", key, "route", route, "cost", cost, "limit", d.Limit)
				writeQuotaExhausted(w, d, cost)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// excluded reports whether path starts with any of prefixes.
func excluded(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// quotaBypassed reports whether r carries a valid bypass token. Compared in
// constant time, and always false when no token is configured.
func quotaBypassed(r *http.Request, token string) bool {
	if token == "" {
		return false
	}
	supplied := r.Header.Get(quotaBypassHeader)
	if supplied == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(supplied), []byte(token)) == 1
}

// quotaKey identifies the quota subject: the authenticated user where there is
// one, otherwise the client IP. The prefixes keep the two namespaces apart so a
// user ID can never collide with an address.
func quotaKey(r *http.Request) string {
	if id := userIDFromContext(r); id != "" {
		return "u:" + id
	}
	if ip := clientIP(r); ip != "" {
		return "ip:" + ip
	}
	return ""
}

// setQuotaHeaders emits the draft-standard RateLimit headers. Reset is in whole
// seconds and rounds up, so a client that waits exactly that long never wakes
// early into another rejection.
func setQuotaHeaders(w http.ResponseWriter, d QuotaDecision) {
	h := w.Header()
	h.Set("RateLimit-Limit", strconv.Itoa(d.Limit))
	h.Set("RateLimit-Remaining", strconv.Itoa(max(d.Remaining, 0)))
	h.Set("RateLimit-Reset", strconv.Itoa(secondsCeil(d.Reset)))
}

// writeQuotaExhausted writes the 429. The body names the quota that ran out,
// what this request would have cost, and when it resets — a bare 429 tells a
// client nothing it can act on beyond "try again eventually".
func writeQuotaExhausted(w http.ResponseWriter, d QuotaDecision, cost int) {
	retryAfter := max(secondsCeil(d.RetryAfter), 1)
	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)

	// Marshalled from a typed value rather than formatted into the response
	// writer: every field here is an integer we computed, but hand-built JSON
	// on an http.ResponseWriter is indistinguishable from the injectable kind
	// to a reader and to static analysis, and encoding/json is what the rest
	// of the package uses.
	body := quotaExhaustedBody{Success: false}
	body.Error.Code = http.StatusTooManyRequests
	body.Error.Message = "request cost quota exhausted"
	body.Error.Reason = "QUOTA_EXHAUSTED"
	body.Error.Quota = "request-cost"
	body.Error.Cost = cost
	body.Error.Limit = d.Limit
	body.Error.Remaining = max(d.Remaining, 0)
	body.Error.RetryAfterSeconds = retryAfter
	body.Error.ResetSeconds = secondsCeil(d.Reset)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line and headers are already on the wire, so there is
		// nothing left to tell the client. The caller sees a truncated body
		// and retries, which is the same outcome as a dropped connection.
		return
	}
}

// quotaExhaustedBody is the 429 payload. It mirrors the envelope the rest of
// the API returns so a client can parse errors one way.
type quotaExhaustedBody struct {
	Success bool `json:"success"`
	Error   struct {
		Code              int    `json:"code"`
		Message           string `json:"message"`
		Reason            string `json:"reason"`
		Quota             string `json:"quota"`
		Cost              int    `json:"cost"`
		Limit             int    `json:"limit"`
		Remaining         int    `json:"remaining"`
		RetryAfterSeconds int    `json:"retry_after_seconds"`
		ResetSeconds      int    `json:"reset_seconds"`
	} `json:"error"`
}

// secondsCeil rounds a duration up to whole seconds, never below zero.
func secondsCeil(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(math.Ceil(d.Seconds()))
}
