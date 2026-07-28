package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
)

// ErrIdempotencyKeyNotFound is returned by IdempotencyStore.Get when no row
// exists for the given (userID, key) — e.g. it expired and was purged.
var ErrIdempotencyKeyNotFound = errors.New("idempotency key not found")

// IdempotencyRecord is a stored idempotency key's current state.
type IdempotencyRecord struct {
	Fingerprint         string
	Status              string // "in_progress" | "completed"
	ResponseStatus      int
	ResponseBody        []byte
	ResponseContentType string
}

// IdempotencyStore is the persistence port the middleware uses. The
// concrete implementation (postgres.IdempotencyRepository) is
// Postgres-backed so completed keys survive a Redis flush/restart — see
// migrations/069_create_idempotency_keys.up.sql.
type IdempotencyStore interface {
	// Claim atomically claims (userID, key). claimed=true means the caller
	// now owns the key and must execute the handler and call Complete
	// (or Release on failure). claimed=false means the key already exists;
	// existing describes its current state.
	Claim(ctx context.Context, userID uuid.UUID, key, fingerprint string, ttl time.Duration) (claimed bool, existing IdempotencyRecord, err error)
	Complete(ctx context.Context, userID uuid.UUID, key string, status int, body []byte, contentType string) error
	Release(ctx context.Context, userID uuid.UUID, key string) error
	Get(ctx context.Context, userID uuid.UUID, key string) (IdempotencyRecord, error)
}

const (
	// idempotencyHeader is the client-supplied opaque key.
	idempotencyHeader = "Idempotency-Key"
	// defaultIdempotencyTTL is how long a completed key's response is
	// replayed for a repeated request — long enough to cover realistic
	// client retries (24h is the value the issue itself calls standard).
	defaultIdempotencyTTL = 24 * time.Hour
	// concurrentWaitTotal/-Poll bound how long a request waits for a
	// concurrent in-progress claim (by another goroutine/instance) to
	// finish before giving up and returning 425, rather than either
	// blocking indefinitely or executing the handler a second time.
	concurrentWaitTotal = 2 * time.Second
	concurrentWaitPoll  = 100 * time.Millisecond
)

// IdempotencyMiddleware makes every request matching routes safe to retry:
// a client-supplied Idempotency-Key header is required on those routes: the
// first request for a (user, key) pair executes the handler and its
// response is durably stored; any repeat of the same key returns that
// stored response without re-executing the handler. A key reused with a
// materially different request (method+path+body fingerprint) is rejected
// with 409 rather than silently returning the wrong stored response.
// Concurrent requests for the same key never both execute the handler —
// Postgres's INSERT ... ON CONFLICT is the atomic gate, so this holds
// across instances, not just within one process.
//
// Must run after the auth middleware (Authenticate), since a key is scoped
// per authenticated user — two users' identical keys never collide.
func IdempotencyMiddleware(store IdempotencyStore, routes []RouteMatch) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !matchesRoute(routes, r) {
				next.ServeHTTP(w, r)
				return
			}

			user, ok := auth.GetUserFromContext(r.Context())
			if !ok {
				// This route requires auth to run first; if it hasn't,
				// that's a wiring bug, not a client error — fail closed
				// rather than silently skip idempotency.
				http.Error(w, "idempotency middleware requires an authenticated request", http.StatusInternalServerError)
				return
			}
			userID, err := uuid.Parse(user.ID)
			if err != nil {
				http.Error(w, "idempotency middleware requires an authenticated request", http.StatusInternalServerError)
				return
			}

			key := r.Header.Get(idempotencyHeader)
			if key == "" {
				http.Error(w, "Idempotency-Key header is required for this endpoint", http.StatusBadRequest)
				return
			}

			bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				http.Error(w, "failed to read request body", http.StatusBadRequest)
				return
			}
			r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

			fingerprint := fingerprintRequest(r.Method, r.URL.Path, bodyBytes)

			claimed, existing, err := store.Claim(r.Context(), userID, key, fingerprint, defaultIdempotencyTTL)
			if err != nil {
				http.Error(w, "idempotency check failed", http.StatusInternalServerError)
				return
			}

			if !claimed {
				serveExisting(w, r, store, userID, key, fingerprint, existing)
				return
			}

			rec := newRecordingResponseWriter(w)
			func() {
				defer func() {
					if p := recover(); p != nil {
						// The claim must not be left permanently
						// in_progress by a panicking handler — that would
						// wedge every future retry of this key behind a
						// claim that can never complete. Release it (a
						// TTL-bounded backstop exists either way) and
						// re-panic so RecoverPanic still handles the 500.
						_ = store.Release(context.Background(), userID, key)
						panic(p)
					}
				}()
				next.ServeHTTP(rec, r)
			}()

			if err := store.Complete(r.Context(), userID, key, rec.status, rec.body.Bytes(), rec.header.Get("Content-Type")); err != nil {
				// The response was already sent to the client at this
				// point (rec wraps the real ResponseWriter and forwards
				// writes through immediately) — a failure to persist here
				// only means a *future* retry might re-execute the
				// handler, not that this request's response was lost.
				_ = store.Release(context.Background(), userID, key)
			}
		})
	}
}

// serveExisting handles the !claimed branch: reject on fingerprint
// mismatch, replay a completed response, or wait briefly for a concurrent
// in-progress claim before giving up with 425.
func serveExisting(
	w http.ResponseWriter,
	r *http.Request,
	store IdempotencyStore,
	userID uuid.UUID,
	key, fingerprint string,
	existing IdempotencyRecord,
) {
	if existing.Fingerprint != fingerprint {
		http.Error(w, "Idempotency-Key was already used with a different request", http.StatusConflict)
		return
	}

	if existing.Status == "completed" {
		writeStoredResponse(w, existing)
		return
	}

	// status == "in_progress": another request (concurrent retry, possibly
	// on a different instance) is currently executing the handler. Wait
	// briefly rather than either blocking indefinitely or running the
	// handler a second time.
	deadline := time.Now().Add(concurrentWaitTotal)
	for time.Now().Before(deadline) {
		time.Sleep(concurrentWaitPoll)
		rec, err := store.Get(r.Context(), userID, key)
		if err != nil {
			if errors.Is(err, ErrIdempotencyKeyNotFound) {
				// The in-progress claim was released (e.g. the original
				// handler panicked) — nothing to wait for anymore. The
				// caller can safely retry, since no completed response
				// exists yet.
				break
			}
			continue
		}
		if rec.Status == "completed" {
			writeStoredResponse(w, rec)
			return
		}
	}

	w.Header().Set("Retry-After", "1")
	http.Error(w, "a request with this Idempotency-Key is already in progress; retry shortly", http.StatusTooEarly)
}

func writeStoredResponse(w http.ResponseWriter, rec IdempotencyRecord) {
	if rec.ResponseContentType != "" {
		w.Header().Set("Content-Type", rec.ResponseContentType)
	}
	w.Header().Set("Idempotency-Replayed", "true")
	status := rec.ResponseStatus
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(rec.ResponseBody)
}

// fingerprintRequest hashes method+path+body so a key reused with a
// materially different request is detected (#835's fingerprinting guard).
func fingerprintRequest(method, path string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte{0})
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// recordingResponseWriter forwards every write to the real
// http.ResponseWriter immediately (so the client sees the response exactly
// as it would without this middleware, with no added latency), while also
// buffering a copy so it can be durably stored after the handler returns.
type recordingResponseWriter struct {
	http.ResponseWriter
	header      http.Header
	status      int
	body        bytes.Buffer
	wroteHeader bool
}

func newRecordingResponseWriter(w http.ResponseWriter) *recordingResponseWriter {
	return &recordingResponseWriter{ResponseWriter: w, header: w.Header(), status: http.StatusOK}
}

func (rw *recordingResponseWriter) WriteHeader(status int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *recordingResponseWriter) Write(p []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	rw.body.Write(p)
	return rw.ResponseWriter.Write(p)
}
