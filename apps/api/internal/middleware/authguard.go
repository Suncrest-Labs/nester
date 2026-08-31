package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/suncrestlabs/nester/apps/api/internal/metrics"
)

// maxAuthBodyPeek bounds how much of a challenge/verify body is buffered to
// read the wallet address out of it. Both bodies are a handful of short JSON
// fields; anything larger is not a legitimate request, and reading it in full
// to find a rate-limit key would itself be the memory-exhaustion vector this
// middleware exists to close.
const maxAuthBodyPeek = 8 << 10 // 8 KiB

// AuthGuardStage identifies which auth endpoint a guard protects, for metrics
// and for keying.
type AuthGuardStage struct {
	// Stage is metrics.AuthStageChallenge or metrics.AuthStageVerify.
	Stage string
	Route RouteMatch
}

// AuthGuardRecorder receives the lockout observations. *metrics.Metrics
// satisfies it; tests pass a fake.
type AuthGuardRecorder interface {
	RecordAuthFailure(scope, stage string)
	RecordAuthLockout(scope, stage string)
	RecordAuthLockedRequest(scope, stage string)
}

// AuthGuard hardens the auth challenge/verify handshake (nester#1104).
//
// It layers three controls on top of the per-IP request limiter from #782,
// which stays in place and is unchanged:
//
//  1. A per-wallet request limit. The existing limiter keys only on IP, so a
//     distributed client could flood the challenge store for one wallet from
//     many addresses without ever tripping it.
//  2. A progressive lockout on repeated failures, tracked per wallet and per
//     IP independently. See authlockout.go for why the backoff is capped.
//  3. Lockout observability, so the control is visible rather than silent.
//
// The guard sits in front of the handler and inspects the response status to
// decide what counts as a failure, so it needs no cooperation from the handler
// itself: any 4xx on verify is a failed authentication attempt. A 5xx is the
// server's fault and is deliberately not counted against the user.
type AuthGuard struct {
	// walletLimiter bounds request rate per claimed wallet address.
	walletLimiter Limiter
	// walletLockout and ipLockout track failures on their respective keys.
	walletLockout AuthLockout
	ipLockout     AuthLockout
	recorder      AuthGuardRecorder
	stages        []AuthGuardStage
}

// NewAuthGuard builds the middleware. stages lists the routes it applies to;
// requests matching none of them pass through untouched.
func NewAuthGuard(
	walletLimiter Limiter,
	walletLockout, ipLockout AuthLockout,
	recorder AuthGuardRecorder,
	stages []AuthGuardStage,
) *AuthGuard {
	return &AuthGuard{
		walletLimiter: walletLimiter,
		walletLockout: walletLockout,
		ipLockout:     ipLockout,
		recorder:      recorder,
		stages:        stages,
	}
}

// Middleware returns the http middleware func.
func (g *AuthGuard) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			stage, ok := g.stageFor(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			wallet, restore, err := peekWalletAddress(r)
			if err != nil {
				// A body we cannot read is the handler's problem to report;
				// pass it on rather than masking a 400 behind a 429.
				next.ServeHTTP(w, r)
				return
			}
			r = restore

			ip := clientIP(r)

			// An active lockout short-circuits before any work is done — this
			// is what stops the challenge store being flooded, because a
			// locked-out caller never reaches the handler that would write to
			// it.
			if wallet != "" {
				if locked, wait := g.walletLockout.Locked(r.Context(), wallet); locked {
					g.record(func(rec AuthGuardRecorder) {
						rec.RecordAuthLockedRequest(metrics.AuthScopeWallet, stage.Stage)
					})
					writeRateLimited(w, wait, "too many failed authentication attempts for this wallet")
					return
				}
			}
			if locked, wait := g.ipLockout.Locked(r.Context(), ip); locked {
				g.record(func(rec AuthGuardRecorder) {
					rec.RecordAuthLockedRequest(metrics.AuthScopeIP, stage.Stage)
				})
				writeRateLimited(w, wait, "too many failed authentication attempts from this client")
				return
			}

			// Per-wallet rate limit. Complements, rather than replaces, the
			// per-IP limiter already wired on these routes.
			if wallet != "" && g.walletLimiter != nil {
				if allowed, wait := g.walletLimiter.Allow(r.Context(), wallet); !allowed {
					writeRateLimited(w, wait, "authentication rate limit exceeded for this wallet")
					return
				}
			}

			// statusRecorder is the shared recorder defined in logging.go; a
			// handler that never calls WriteHeader leaves the seeded 200.
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(recorder, r)

			g.observe(r, stage, wallet, ip, recorder.status)
		})
	}
}

// observe turns the handler's status into failure/success bookkeeping.
//
// Only 4xx counts as a failure: it is the class that covers a bad signature, an
// expired or replayed challenge, and a malformed request, all of which are
// things a brute-force produces in volume. 5xx is excluded so an outage cannot
// lock every user out, and 429 is excluded so an already-throttled client does
// not have its own throttling counted as fresh failures.
func (g *AuthGuard) observe(r *http.Request, stage AuthGuardStage, wallet, ip string, status int) {
	if status < 400 {
		// A successful authentication clears the slate for both keys, so a
		// user who mistyped or retried a flaky wallet signature is not left
		// carrying a backoff.
		if wallet != "" {
			g.walletLockout.Reset(r.Context(), wallet)
		}
		g.ipLockout.Reset(r.Context(), ip)
		return
	}
	if status >= 500 || status == http.StatusTooManyRequests {
		return
	}

	if wallet != "" {
		g.recordFailure(r, g.walletLockout, wallet, metrics.AuthScopeWallet, stage.Stage)
	}
	g.recordFailure(r, g.ipLockout, ip, metrics.AuthScopeIP, stage.Stage)
}

func (g *AuthGuard) recordFailure(r *http.Request, lockout AuthLockout, key, scope, stage string) {
	g.record(func(rec AuthGuardRecorder) { rec.RecordAuthFailure(scope, stage) })

	if _, justLocked := lockout.RecordFailure(r.Context(), key); justLocked {
		g.record(func(rec AuthGuardRecorder) { rec.RecordAuthLockout(scope, stage) })
	}
}

func (g *AuthGuard) record(fn func(AuthGuardRecorder)) {
	if g.recorder == nil {
		return
	}
	fn(g.recorder)
}

func (g *AuthGuard) stageFor(r *http.Request) (AuthGuardStage, bool) {
	for _, s := range g.stages {
		if r.Method == s.Route.Method && matchPathPattern(s.Route.Path, r.URL.Path) {
			return s, true
		}
	}
	return AuthGuardStage{}, false
}

// peekWalletAddress reads wallet_address out of a JSON body and returns a
// request whose body can still be read in full by the handler.
//
// The body is bounded by maxAuthBodyPeek and restored from the buffer, so the
// handler behaves exactly as it would without this middleware — including
// seeing an oversized body and rejecting it itself.
func peekWalletAddress(r *http.Request) (string, *http.Request, error) {
	if r.Body == nil {
		return "", r, nil
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxAuthBodyPeek+1))
	if err != nil {
		return "", r, err
	}
	_ = r.Body.Close()

	// Hand the bytes back to the handler either way.
	r.Body = io.NopCloser(bytes.NewReader(body))

	if len(body) > maxAuthBodyPeek {
		// Too large to be a real auth request. Fall through with no wallet
		// key; the IP-scoped controls still apply and the handler will reject
		// the body on its own terms.
		return "", r, nil
	}

	var payload struct {
		WalletAddress string `json:"wallet_address"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", r, nil
	}
	return strings.TrimSpace(payload.WalletAddress), r, nil
}
