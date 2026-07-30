package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
)

// fakeIdempotencyStore is an in-memory IdempotencyStore whose Claim uses a
// real mutex to atomically test-and-set, the same way the real
// Postgres INSERT ... ON CONFLICT does — so concurrency tests against it
// are meaningful, not just theater.
type fakeIdempotencyStore struct {
	mu      sync.Mutex
	records map[string]IdempotencyRecord

	// completeFailuresRemaining, when > 0, makes the next that-many calls to
	// Complete return errCompleteFailed instead of succeeding — simulates a
	// transient DB hiccup for completeWithRetry tests.
	completeFailuresRemaining int
	completeCalls             int
	releaseCalls              int
}

func newFakeIdempotencyStore() *fakeIdempotencyStore {
	return &fakeIdempotencyStore{records: make(map[string]IdempotencyRecord)}
}

var errCompleteFailed = errors.New("simulated transient Complete failure")

func fakeKey(userID uuid.UUID, key string) string { return userID.String() + ":" + key }

func (s *fakeIdempotencyStore) Claim(_ context.Context, userID uuid.UUID, key, fingerprint string, _ time.Duration) (bool, IdempotencyRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := fakeKey(userID, key)
	if existing, ok := s.records[k]; ok {
		return false, existing, nil
	}
	s.records[k] = IdempotencyRecord{Fingerprint: fingerprint, Status: "in_progress"}
	return true, IdempotencyRecord{}, nil
}

func (s *fakeIdempotencyStore) Complete(_ context.Context, userID uuid.UUID, key string, status int, body []byte, contentType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completeCalls++
	if s.completeFailuresRemaining > 0 {
		s.completeFailuresRemaining--
		return errCompleteFailed
	}
	k := fakeKey(userID, key)
	rec := s.records[k]
	rec.Status = "completed"
	rec.ResponseStatus = status
	rec.ResponseBody = body
	rec.ResponseContentType = contentType
	s.records[k] = rec
	return nil
}

func (s *fakeIdempotencyStore) Release(_ context.Context, userID uuid.UUID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseCalls++
	delete(s.records, fakeKey(userID, key))
	return nil
}

func (s *fakeIdempotencyStore) Get(_ context.Context, userID uuid.UUID, key string) (IdempotencyRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[fakeKey(userID, key)]
	if !ok {
		return IdempotencyRecord{}, ErrIdempotencyKeyNotFound
	}
	return rec, nil
}

var testUserID = uuid.New()

func withTestUser(req *http.Request) *http.Request {
	ctx := auth.NewContext(req.Context(), auth.User{ID: testUserID.String()})
	return req.WithContext(ctx)
}

func idempotencyRoutes() []RouteMatch {
	return []RouteMatch{{Method: http.MethodPost, Path: "/api/v1/transactions"}}
}

func TestIdempotencyMiddleware_PassesThroughUnmatchedRoutes(t *testing.T) {
	store := newFakeIdempotencyStore()
	var calls int32
	handler := IdempotencyMiddleware(store, idempotencyRoutes())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions", nil) // GET, not POST -> unmatched
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected handler called once, got %d", calls)
	}
}

func TestIdempotencyMiddleware_RequiresHeaderOnMatchedRoute(t *testing.T) {
	store := newFakeIdempotencyStore()
	handler := IdempotencyMiddleware(store, idempotencyRoutes())(ok200)

	req := withTestUser(httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(`{"a":1}`)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400 for missing Idempotency-Key", rec.Code)
	}
}

func TestIdempotencyMiddleware_FirstRequestExecutesHandler(t *testing.T) {
	store := newFakeIdempotencyStore()
	var calls int32
	handler := IdempotencyMiddleware(store, idempotencyRoutes())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"tx-1"}`))
	}))

	req := withTestUser(httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(`{"a":1}`)))
	req.Header.Set("Idempotency-Key", "key-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201", rec.Code)
	}
	if rec.Body.String() != `{"id":"tx-1"}` {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected handler called once, got %d", calls)
	}
}

func TestIdempotencyMiddleware_RepeatedKeyReturnsStoredResponseWithoutReexecuting(t *testing.T) {
	store := newFakeIdempotencyStore()
	var calls int32
	handler := IdempotencyMiddleware(store, idempotencyRoutes())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"tx-1"}`))
	}))

	makeReq := func() *http.Request {
		req := withTestUser(httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(`{"a":1}`)))
		req.Header.Set("Idempotency-Key", "key-1")
		return req
	}

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, makeReq())

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, makeReq())

	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected handler called exactly once across both requests, got %d", calls)
	}
	if second.Code != first.Code || second.Body.String() != first.Body.String() {
		t.Fatalf("expected identical replayed response: first=%d/%s second=%d/%s",
			first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	if second.Header().Get("Idempotency-Replayed") != "true" {
		t.Error("expected Idempotency-Replayed: true on the replayed response")
	}
}

func TestIdempotencyMiddleware_SameKeyDifferentBodyIsRejected(t *testing.T) {
	store := newFakeIdempotencyStore()
	handler := IdempotencyMiddleware(store, idempotencyRoutes())(ok200)

	req1 := withTestUser(httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(`{"amount":1}`)))
	req1.Header.Set("Idempotency-Key", "key-1")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := withTestUser(httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(`{"amount":999}`)))
	req2.Header.Set("Idempotency-Key", "key-1")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Fatalf("got %d, want 409 for reused key with a different body", rec2.Code)
	}
}

func TestIdempotencyMiddleware_ConcurrentRetriesExecuteHandlerExactlyOnce(t *testing.T) {
	store := newFakeIdempotencyStore()
	var calls int32
	started := make(chan struct{})
	release := make(chan struct{})

	handler := IdempotencyMiddleware(store, idempotencyRoutes())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			close(started)
			<-release // hold the first caller inside the handler
		}
		w.WriteHeader(http.StatusCreated)
	}))

	makeReq := func() *http.Request {
		req := withTestUser(httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(`{"a":1}`)))
		req.Header.Set("Idempotency-Key", "concurrent-key")
		return req
	}

	var wg sync.WaitGroup
	codes := make([]int, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, makeReq())
		codes[0] = rec.Code
	}()

	<-started // first request is now inside the handler, holding the claim

	wg.Add(1)
	go func() {
		defer wg.Done()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, makeReq())
		codes[1] = rec.Code
		close(release) // let the first request finish once the second has responded (425)
	}()

	wg.Wait()

	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected the handler to execute exactly once under concurrent retries, got %d", calls)
	}
	if codes[1] != http.StatusTooEarly {
		t.Errorf("expected the concurrent retry to get 425 Too Early while the first is in flight, got %d", codes[1])
	}
	if codes[0] != http.StatusCreated {
		t.Errorf("expected the original request to still complete normally, got %d", codes[0])
	}
}

func TestIdempotencyMiddleware_FailsClosedWithoutAuthenticatedUser(t *testing.T) {
	store := newFakeIdempotencyStore()
	handler := IdempotencyMiddleware(store, idempotencyRoutes())(ok200)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(`{}`)) // no auth context
	req.Header.Set("Idempotency-Key", "key-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500 when auth context is missing (fail closed)", rec.Code)
	}
}

func TestIdempotencyMiddleware_RejectsOversizedBodyWithoutTruncating(t *testing.T) {
	store := newFakeIdempotencyStore()
	var calls int32
	handler := IdempotencyMiddleware(store, idempotencyRoutes())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusCreated)
	}))

	// One byte over maxIdempotencyBodyBytes: must be rejected outright, not
	// silently truncated to the limit and forwarded as a shorter-but-valid
	// body — see the maxIdempotencyBodyBytes+1 read in the middleware.
	oversized := strings.Repeat("a", maxIdempotencyBodyBytes+1)
	req := withTestUser(httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(oversized)))
	req.Header.Set("Idempotency-Key", "key-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d, want 413 for a body over maxIdempotencyBodyBytes", rec.Code)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("expected handler NOT to run for a rejected oversized body, got %d calls", calls)
	}
}

func TestIdempotencyMiddleware_AcceptsBodyAtExactSizeLimit(t *testing.T) {
	store := newFakeIdempotencyStore()
	var calls int32
	handler := IdempotencyMiddleware(store, idempotencyRoutes())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusCreated)
	}))

	exact := strings.Repeat("a", maxIdempotencyBodyBytes)
	req := withTestUser(httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(exact)))
	req.Header.Set("Idempotency-Key", "key-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201 for a body exactly at maxIdempotencyBodyBytes", rec.Code)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected handler called once, got %d", calls)
	}
}

func TestIdempotencyMiddleware_CompleteRetriesThroughTransientFailures(t *testing.T) {
	store := newFakeIdempotencyStore()
	store.completeFailuresRemaining = completeMaxRetries - 1 // succeeds on the last attempt
	handler := IdempotencyMiddleware(store, idempotencyRoutes())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := withTestUser(httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(`{}`)))
	req.Header.Set("Idempotency-Key", "key-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201", rec.Code)
	}
	if store.completeCalls != completeMaxRetries {
		t.Errorf("expected %d Complete attempts, got %d", completeMaxRetries, store.completeCalls)
	}
	if store.releaseCalls != 0 {
		t.Errorf("expected no Release once Complete eventually succeeds, got %d calls", store.releaseCalls)
	}

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("retry after successful Complete: got %d, want the stored 201", rec2.Code)
	}
	if store.completeCalls != completeMaxRetries {
		t.Errorf("a replayed request must not call Complete again, got %d total calls", store.completeCalls)
	}
}

func TestIdempotencyMiddleware_CompleteFallsBackToReleaseAfterExhaustingRetries(t *testing.T) {
	store := newFakeIdempotencyStore()
	store.completeFailuresRemaining = completeMaxRetries + 5 // never succeeds within the retry budget
	handler := IdempotencyMiddleware(store, idempotencyRoutes())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := withTestUser(httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(`{}`)))
	req.Header.Set("Idempotency-Key", "key-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// The client still gets the real response — Complete failing is a
	// persistence problem, not a request failure.
	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201 even though Complete never persisted", rec.Code)
	}
	if store.completeCalls != completeMaxRetries {
		t.Errorf("expected exactly %d Complete attempts before giving up, got %d", completeMaxRetries, store.completeCalls)
	}
	if store.releaseCalls != 1 {
		t.Errorf("expected exactly one Release after exhausting Complete retries, got %d", store.releaseCalls)
	}
}

func TestFingerprintRequest_SameInputsSameFingerprint(t *testing.T) {
	a := fingerprintRequest("POST", "/x", []byte(`{"a":1}`))
	b := fingerprintRequest("POST", "/x", []byte(`{"a":1}`))
	if a != b {
		t.Error("expected identical inputs to produce the same fingerprint")
	}
}

func TestFingerprintRequest_DifferentBodyDifferentFingerprint(t *testing.T) {
	a := fingerprintRequest("POST", "/x", []byte(`{"a":1}`))
	b := fingerprintRequest("POST", "/x", []byte(`{"a":2}`))
	if a == b {
		t.Error("expected different bodies to produce different fingerprints")
	}
}
