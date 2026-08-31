package middleware

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Auth-failure lockout (nester#1104).
//
// This sits on top of the per-IP request limiter from #782, and is a different
// control: the limiter bounds how FAST anyone may call challenge/verify, this
// bounds how many times they may FAIL. A signature brute-force that stays under
// the rate limit is still a brute-force, and slowing down does not help the
// attacker here because the backoff escalates with the failure count rather
// than resetting with time.
//
// Two keys are tracked independently for each attempt: the claimed wallet
// address and the client IP. A distributed attack from many IPs against one
// wallet is caught by the wallet key; a single host spraying many wallets is
// caught by the IP key.
//
// The backoff doubles per failure past the threshold, from Base, capped at Max.
// The cap matters: without it, anyone could permanently deny service to a
// wallet address they do not control simply by failing verify against it
// repeatedly. With it, the worst an attacker can impose on a third party is a
// Max-long delay, while their own IP key locks out just as hard.

// AuthLockout tracks authentication failures and decides whether a key is
// currently locked out.
//
// Implementations must be safe for concurrent use.
type AuthLockout interface {
	// Locked reports whether key is currently locked out, and for how long.
	Locked(ctx context.Context, key string) (locked bool, retryAfter time.Duration)
	// RecordFailure registers one failure for key and returns the lockout it
	// now carries. justLocked is true only on the transition into a locked
	// state, so a caller can count lockouts without double-counting every
	// subsequent rejected request.
	RecordFailure(ctx context.Context, key string) (lockout time.Duration, justLocked bool)
	// Reset clears the failure history for key. Called after a successful
	// authentication so a user who eventually signs in correctly is not
	// penalised for earlier fumbles.
	Reset(ctx context.Context, key string)
}

// AuthLockoutConfig parameterises the progressive backoff.
type AuthLockoutConfig struct {
	// Threshold is the number of failures tolerated before lockouts begin.
	Threshold int
	// Window is how long a failure counts toward the threshold.
	Window time.Duration
	// Base is the first lockout duration, applied at Threshold+1 failures.
	Base time.Duration
	// Max caps the doubling.
	Max time.Duration
}

// lockoutFor computes the backoff for a given cumulative failure count:
// zero until the threshold is passed, then Base doubled once per extra
// failure, capped at Max.
func (c AuthLockoutConfig) lockoutFor(failures int) time.Duration {
	if failures <= c.Threshold {
		return 0
	}

	lockout := c.Base
	// Doubling is done by repeated multiplication with an early exit at Max
	// rather than a shift, so a large failure count cannot overflow the
	// duration into a negative value.
	for i := 0; i < failures-c.Threshold-1; i++ {
		if lockout >= c.Max {
			return c.Max
		}
		lockout *= 2
	}
	if lockout > c.Max {
		return c.Max
	}
	return lockout
}

// NewAuthLockout returns a Redis-backed tracker when rc is non-nil (so the
// lockout holds across every API instance, which is the only way it bounds a
// distributed attack) and a process-local one otherwise.
//
// prefix namespaces the keys so the wallet-scoped and IP-scoped trackers do not
// collide.
func NewAuthLockout(rc *redis.Client, prefix string, cfg AuthLockoutConfig) AuthLockout {
	if rc != nil {
		return &redisAuthLockout{rc: rc, prefix: prefix, cfg: cfg}
	}
	return newMemoryAuthLockout(cfg)
}

// ── Redis implementation ─────────────────────────────────────────────────────

type redisAuthLockout struct {
	rc     *redis.Client
	prefix string
	cfg    AuthLockoutConfig
}

func (l *redisAuthLockout) failKey(key string) string { return "authfail:" + l.prefix + ":" + key }
func (l *redisAuthLockout) lockKey(key string) string { return "authlock:" + l.prefix + ":" + key }

func (l *redisAuthLockout) Locked(ctx context.Context, key string) (bool, time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, redisCallTimeout)
	defer cancel()

	ttl, err := l.rc.PTTL(ctx, l.lockKey(key)).Result()
	if err != nil {
		// Fail open, consistent with the rate limiter: a Redis outage must not
		// lock every user out of the product. The per-IP request limiter still
		// applies, so the endpoint is not left unprotected.
		slog.Default().WarnContext(ctx, "auth lockout: redis error, failing open",
			"prefix", l.prefix, "error", err)
		return false, 0
	}
	if ttl <= 0 {
		return false, 0
	}
	return true, ttl
}

// authFailureScript increments the failure counter, (re)sets its window TTL,
// and returns the resulting count. Keeping increment and expiry in one script
// means a counter can never be left without a TTL and so leak forever.
var authFailureScript = redis.NewScript(`
local failures = redis.call("INCR", KEYS[1])
redis.call("PEXPIRE", KEYS[1], ARGV[1])
return failures
`)

func (l *redisAuthLockout) RecordFailure(ctx context.Context, key string) (time.Duration, bool) {
	ctx, cancel := context.WithTimeout(ctx, redisCallTimeout)
	defer cancel()

	res, err := authFailureScript.Run(ctx, l.rc,
		[]string{l.failKey(key)}, l.cfg.Window.Milliseconds()).Result()
	if err != nil {
		slog.Default().WarnContext(ctx, "auth lockout: redis error recording failure, failing open",
			"prefix", l.prefix, "error", err)
		return 0, false
	}

	failures, _ := res.(int64)
	lockout := l.cfg.lockoutFor(int(failures))
	if lockout <= 0 {
		return 0, false
	}

	// SET NX reports whether this call created the lock, which is exactly the
	// transition the lockout metric should count. A later failure extends the
	// lock via a plain SET without re-counting it.
	created, err := l.rc.SetNX(ctx, l.lockKey(key), "1", lockout).Result()
	if err != nil {
		slog.Default().WarnContext(ctx, "auth lockout: redis error setting lock",
			"prefix", l.prefix, "error", err)
		return lockout, false
	}
	if !created {
		// Already locked; extend to the newly computed (longer) backoff.
		if err := l.rc.Set(ctx, l.lockKey(key), "1", lockout).Err(); err != nil {
			slog.Default().WarnContext(ctx, "auth lockout: redis error extending lock",
				"prefix", l.prefix, "error", err)
		}
	}
	return lockout, created
}

func (l *redisAuthLockout) Reset(ctx context.Context, key string) {
	ctx, cancel := context.WithTimeout(ctx, redisCallTimeout)
	defer cancel()

	if err := l.rc.Del(ctx, l.failKey(key), l.lockKey(key)).Err(); err != nil {
		slog.Default().WarnContext(ctx, "auth lockout: redis error clearing failures",
			"prefix", l.prefix, "error", err)
	}
}

// ── In-memory implementation (dev / single-instance fallback) ────────────────

type memoryAuthEntry struct {
	failures    int
	failuresEnd time.Time
	lockedUntil time.Time
}

type memoryAuthLockout struct {
	mu  sync.Mutex
	m   map[string]*memoryAuthEntry
	cfg AuthLockoutConfig
}

func newMemoryAuthLockout(cfg AuthLockoutConfig) *memoryAuthLockout {
	return &memoryAuthLockout{m: make(map[string]*memoryAuthEntry), cfg: cfg}
}

func (l *memoryAuthLockout) Locked(_ context.Context, key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.m[key]
	if !ok {
		return false, 0
	}
	now := time.Now()
	if entry.lockedUntil.After(now) {
		return true, time.Until(entry.lockedUntil)
	}
	return false, 0
}

func (l *memoryAuthLockout) RecordFailure(_ context.Context, key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	l.evictExpiredLocked(now)

	entry, ok := l.m[key]
	if !ok || now.After(entry.failuresEnd) {
		entry = &memoryAuthEntry{}
		l.m[key] = entry
	}
	entry.failures++
	entry.failuresEnd = now.Add(l.cfg.Window)

	lockout := l.cfg.lockoutFor(entry.failures)
	if lockout <= 0 {
		return 0, false
	}

	justLocked := !entry.lockedUntil.After(now)
	entry.lockedUntil = now.Add(lockout)
	return lockout, justLocked
}

func (l *memoryAuthLockout) Reset(_ context.Context, key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, key)
}

// evictExpiredLocked drops entries whose failure window and lockout have both
// passed. Without it the map grows once per distinct wallet address or IP an
// attacker chooses to use, which is itself a memory-exhaustion vector.
//
// The caller must hold l.mu.
func (l *memoryAuthLockout) evictExpiredLocked(now time.Time) {
	for key, entry := range l.m {
		if now.After(entry.failuresEnd) && now.After(entry.lockedUntil) {
			delete(l.m, key)
		}
	}
}
