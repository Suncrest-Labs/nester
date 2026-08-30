package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The health endpoints are the contract every orchestrator, load balancer, and
// runbook in the fleet is written against, and the only one nobody
// authenticates to reach. This file pins all four of them — /healthz (the
// canonical liveness path), /health (its permanent alias), /readyz, and
// /health/detailed — against status code, response body, Content-Type, and the
// exact JSON key set, in both healthy and degraded states.
//
// Degradation is driven entirely by stubs: healthDeps takes dependency probes
// rather than a *pgxpool.Pool and a *redis.Client, so "PostgreSQL is down" is
// a probe that returns an error and no test has to stop a real database. The
// Stellar probes are pointed at httptest servers for the same reason.

const (
	// livenessContentType and detailedContentType are the Content-Type values
	// the endpoints have always sent. Probes and dashboards parse on them, so
	// they are part of the contract, not an implementation detail.
	livenessContentType = "text/plain; charset=utf-8"
	detailedContentType = "application/json"

	detailedPath = "/health/detailed"
)

// leakyDBError and leakyRedisError mimic what the drivers actually return when
// they cannot dial: pgx puts the DSN's user, host, password, and database name
// into the error string, and go-redis puts the resolved address into its own.
// If a handler ever echoes a driver error verbatim, these are the tokens that
// reach an unauthenticated caller.
//
// The fixture values carry the SENTINEL- prefix this repository already uses
// for look-alike secrets in tests (see .gitleaksignore and
// internal/config/redact_test.go): they are not credentials and grant access
// to nothing, and the prefix says so at a glance in a leak report.
var (
	leakyDBError = errors.New("failed to connect to `host=postgres.internal.nester.svc " +
		"user=nester password=SENTINEL-fake-db-password database=nester_prod`: dial error")
	leakyRedisError = errors.New("dial tcp redis.internal.nester.svc:6379: connect: connection refused")
)

// upstreamLeakBody is what a failing Horizon / Soroban RPC node returns. The
// Stellar probes copy up to 512 bytes of it into their Error field, so the
// health handler must not pass that field through.
const upstreamLeakBody = `{"detail":"upstream refused by host=horizon.internal.nester.svc token=SENTINEL-upstream-token"}`

// forbiddenInHealthPayload is what must never appear in any health response.
var forbiddenInHealthPayload = []string{
	"password",
	"SENTINEL-fake-db-password",
	"user=nester",
	"host=postgres.internal",
	"postgres.internal.nester.svc",
	"redis.internal.nester.svc",
	"horizon.internal.nester.svc",
	"SENTINEL-upstream-token",
	"connection refused",
	"postgres://",
	"redis://",
	"sslmode=",
}

type healthContractCase struct {
	name string
	path string

	// Dependency state. The zero value is the worst case on purpose: a case
	// has to opt in to "ready" and to healthy Stellar dependencies.
	ready              bool
	dbErr              error
	redisErr           error
	redisNotConfigured bool
	horizonHealthy     bool
	sorobanHealthy     bool

	wantCode        int
	wantContentType string

	// wantBody is the exact response body for the plain-text endpoints
	// (/healthz, /health, /readyz).
	wantBody string

	// The remaining fields apply to /health/detailed only.
	wantStatusField     string
	wantDatabaseOK      bool
	wantDatabaseError   string
	wantRedisOK         bool
	wantRedisConfigured bool
	wantRedisError      string
	wantHorizonOK       bool
	wantSorobanOK       bool
}

func TestHealthEndpointContract(t *testing.T) {
	cases := []healthContractCase{
		// ---- Liveness: /healthz is canonical, /health is its alias. --------
		{
			name: "healthz reports ok while serving", path: "/healthz",
			ready: true, horizonHealthy: true, sorobanHealthy: true,
			wantCode: http.StatusOK, wantContentType: livenessContentType, wantBody: "ok",
		},
		{
			name: "health alias reports ok while serving", path: "/health",
			ready: true, horizonHealthy: true, sorobanHealthy: true,
			wantCode: http.StatusOK, wantContentType: livenessContentType, wantBody: "ok",
		},
		{
			// Liveness follows the drain flag only. A dead database must not
			// make an orchestrator restart an otherwise-live process.
			name: "healthz reports draining after shutdown starts", path: "/healthz",
			dbErr: leakyDBError, redisErr: leakyRedisError,
			wantCode: http.StatusServiceUnavailable, wantContentType: livenessContentType, wantBody: "draining",
		},
		{
			name: "health alias reports draining after shutdown starts", path: "/health",
			wantCode: http.StatusServiceUnavailable, wantContentType: livenessContentType, wantBody: "draining",
		},
		{
			name: "healthz stays ok while dependencies are down", path: "/healthz",
			ready: true, dbErr: leakyDBError, redisErr: leakyRedisError,
			wantCode: http.StatusOK, wantContentType: livenessContentType, wantBody: "ok",
		},

		// ---- Readiness: fails closed on every hard dependency. -------------
		{
			name: "readyz reports ok when every dependency answers", path: "/readyz",
			ready: true, horizonHealthy: true, sorobanHealthy: true,
			wantCode: http.StatusOK, wantContentType: livenessContentType, wantBody: "ok",
		},
		{
			name: "readyz reports draining after shutdown starts", path: "/readyz",
			wantCode: http.StatusServiceUnavailable, wantContentType: livenessContentType, wantBody: "draining",
		},
		{
			name: "readyz fails when the database is unavailable", path: "/readyz",
			ready: true, dbErr: leakyDBError,
			wantCode: http.StatusServiceUnavailable, wantContentType: livenessContentType,
			wantBody: "database unavailable",
		},
		{
			// A saturated pool never dials — it blocks until the probe's own
			// timeout fires, which is what the driver surfaces here.
			name: "readyz fails when the database pool is exhausted", path: "/readyz",
			ready: true, dbErr: context.DeadlineExceeded,
			wantCode: http.StatusServiceUnavailable, wantContentType: livenessContentType,
			wantBody: "database unavailable",
		},
		{
			name: "readyz fails when redis is unavailable", path: "/readyz",
			ready: true, redisErr: leakyRedisError,
			wantCode: http.StatusServiceUnavailable, wantContentType: livenessContentType,
			wantBody: "redis unavailable",
		},
		{
			// Redis is optional: an instance deliberately running on the
			// in-memory fallbacks is ready, not degraded.
			name: "readyz reports ok when redis is not configured", path: "/readyz",
			ready: true, redisNotConfigured: true,
			wantCode: http.StatusOK, wantContentType: livenessContentType, wantBody: "ok",
		},
		{
			// Stellar outages degrade individual routes; they must not pull
			// the whole instance out of rotation.
			name: "readyz stays ok when stellar dependencies are down", path: "/readyz",
			ready:    true,
			wantCode: http.StatusOK, wantContentType: livenessContentType, wantBody: "ok",
		},

		// ---- Detailed diagnostics. -----------------------------------------
		{
			name: "detailed reports ok when everything answers", path: detailedPath,
			ready: true, horizonHealthy: true, sorobanHealthy: true,
			wantCode: http.StatusOK, wantContentType: detailedContentType,
			wantStatusField: "ok",
			wantDatabaseOK:  true,
			wantRedisOK:     true, wantRedisConfigured: true,
			wantHorizonOK: true, wantSorobanOK: true,
		},
		{
			name: "detailed reports the database as down", path: detailedPath,
			ready: true, dbErr: leakyDBError, horizonHealthy: true, sorobanHealthy: true,
			wantCode: http.StatusServiceUnavailable, wantContentType: detailedContentType,
			wantStatusField: "degraded",
			wantDatabaseOK:  false, wantDatabaseError: "unavailable",
			wantRedisOK: true, wantRedisConfigured: true,
			wantHorizonOK: true, wantSorobanOK: true,
		},
		{
			name: "detailed reports a database timeout as a timeout", path: detailedPath,
			ready: true, dbErr: context.DeadlineExceeded, horizonHealthy: true, sorobanHealthy: true,
			wantCode: http.StatusServiceUnavailable, wantContentType: detailedContentType,
			wantStatusField: "degraded",
			wantDatabaseOK:  false, wantDatabaseError: "timeout",
			wantRedisOK: true, wantRedisConfigured: true,
			wantHorizonOK: true, wantSorobanOK: true,
		},
		{
			name: "detailed reports redis as down", path: detailedPath,
			ready: true, redisErr: leakyRedisError, horizonHealthy: true, sorobanHealthy: true,
			wantCode: http.StatusServiceUnavailable, wantContentType: detailedContentType,
			wantStatusField: "degraded",
			wantDatabaseOK:  true,
			wantRedisOK:     false, wantRedisConfigured: true, wantRedisError: "unavailable",
			wantHorizonOK: true, wantSorobanOK: true,
		},
		{
			name: "detailed reports redis as unconfigured without failing", path: detailedPath,
			ready: true, redisNotConfigured: true, horizonHealthy: true, sorobanHealthy: true,
			wantCode: http.StatusOK, wantContentType: detailedContentType,
			wantStatusField: "ok",
			wantDatabaseOK:  true,
			wantRedisOK:     true, wantRedisConfigured: false,
			wantHorizonOK: true, wantSorobanOK: true,
		},
		{
			name: "detailed degrades but stays servable when stellar is down", path: detailedPath,
			ready:    true,
			wantCode: http.StatusOK, wantContentType: detailedContentType,
			wantStatusField: "degraded",
			wantDatabaseOK:  true,
			wantRedisOK:     true, wantRedisConfigured: true,
			wantHorizonOK: false, wantSorobanOK: false,
		},
		{
			name: "detailed reports draining after shutdown starts", path: detailedPath,
			horizonHealthy: true, sorobanHealthy: true,
			wantCode: http.StatusServiceUnavailable, wantContentType: detailedContentType,
			wantStatusField: "draining",
			wantDatabaseOK:  true,
			wantRedisOK:     true, wantRedisConfigured: true,
			wantHorizonOK: true, wantSorobanOK: true,
		},
	}

	assertReadinessCanFail(t, cases)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			registerHealthRoutes(mux, tc.healthDeps(t))

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))

			if rec.Code != tc.wantCode {
				t.Errorf("GET %s status = %d, want %d (body %q)", tc.path, rec.Code, tc.wantCode, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != tc.wantContentType {
				t.Errorf("GET %s Content-Type = %q, want %q", tc.path, got, tc.wantContentType)
			}
			assertNoSecretsLeaked(t, tc.path, rec.Body.String())

			if tc.path != detailedPath {
				if got := rec.Body.String(); got != tc.wantBody {
					t.Errorf("GET %s body = %q, want %q", tc.path, got, tc.wantBody)
				}
				return
			}
			tc.assertDetailedContract(t, rec.Body.Bytes())
		})
	}
}

// assertDetailedContract pins /health/detailed: the exact key set at every
// level, and the reported state of each dependency.
func (tc healthContractCase) assertDetailedContract(t *testing.T, raw []byte) {
	t.Helper()

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("GET %s returned invalid JSON: %v (body %q)", tc.path, err, raw)
	}
	assertKeySet(t, "response", envelope,
		// "commit" joins "version" here: #1117 requires the running build to be
		// identifiable from the status endpoint, not only from the logs.
		[]string{"status", "environment", "version", "commit", "uptime_seconds", "database", "redis", "horizon", "soroban_rpc", "generated_at"},
		nil)

	// latency_ms and error are omitempty, so they are permitted rather than
	// required — but nothing outside these sets may appear.
	assertKeySet(t, "database", mustObject(t, envelope["database"]),
		[]string{"ok", "max_conns", "acquired_conns", "idle_conns", "total_conns"},
		[]string{"latency_ms", "error"})
	assertKeySet(t, "redis", mustObject(t, envelope["redis"]),
		[]string{"ok", "configured"},
		[]string{"latency_ms", "error"})
	for _, field := range []string{"horizon", "soroban_rpc"} {
		assertKeySet(t, field, mustObject(t, envelope[field]),
			[]string{"ok"},
			[]string{"endpoint", "latency_ms", "error", "latest_ledger"})
	}

	var body struct {
		Status      string `json:"status"`
		Environment string `json:"environment"`
		Version     string `json:"version"`
		UptimeSecs  int64  `json:"uptime_seconds"`
		Database    struct {
			OK       bool   `json:"ok"`
			Error    string `json:"error"`
			MaxConns int32  `json:"max_conns"`
		} `json:"database"`
		Redis struct {
			OK         bool   `json:"ok"`
			Configured bool   `json:"configured"`
			Error      string `json:"error"`
		} `json:"redis"`
		Horizon struct {
			OK bool `json:"ok"`
		} `json:"horizon"`
		SorobanRPC struct {
			OK bool `json:"ok"`
		} `json:"soroban_rpc"`
		GeneratedAt time.Time `json:"generated_at"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode %s payload: %v", tc.path, err)
	}

	if body.Status != tc.wantStatusField {
		t.Errorf("status = %q, want %q", body.Status, tc.wantStatusField)
	}
	if body.Database.OK != tc.wantDatabaseOK {
		t.Errorf("database.ok = %t, want %t", body.Database.OK, tc.wantDatabaseOK)
	}
	if body.Database.Error != tc.wantDatabaseError {
		t.Errorf("database.error = %q, want %q", body.Database.Error, tc.wantDatabaseError)
	}
	if body.Redis.OK != tc.wantRedisOK {
		t.Errorf("redis.ok = %t, want %t", body.Redis.OK, tc.wantRedisOK)
	}
	if body.Redis.Configured != tc.wantRedisConfigured {
		t.Errorf("redis.configured = %t, want %t", body.Redis.Configured, tc.wantRedisConfigured)
	}
	if body.Redis.Error != tc.wantRedisError {
		t.Errorf("redis.error = %q, want %q", body.Redis.Error, tc.wantRedisError)
	}
	if body.Horizon.OK != tc.wantHorizonOK {
		t.Errorf("horizon.ok = %t, want %t", body.Horizon.OK, tc.wantHorizonOK)
	}
	if body.SorobanRPC.OK != tc.wantSorobanOK {
		t.Errorf("soroban_rpc.ok = %t, want %t", body.SorobanRPC.OK, tc.wantSorobanOK)
	}

	// The remaining fields are metadata the payload has always carried; they
	// are asserted as present and sane rather than exact.
	if body.Environment != "test" || body.Version != "test-build" {
		t.Errorf("environment/version = %q/%q, want %q/%q", body.Environment, body.Version, "test", "test-build")
	}
	if body.Database.MaxConns == 0 {
		t.Error("database.max_conns = 0, want the pool's configured maximum")
	}
	if body.UptimeSecs <= 0 {
		t.Errorf("uptime_seconds = %d, want a positive uptime", body.UptimeSecs)
	}
	if body.GeneratedAt.IsZero() {
		t.Error("generated_at is zero, want the time the report was produced")
	}
}

// assertReadinessCanFail guards the table itself. A readiness endpoint that
// always answers 200 is worse than none at all — it keeps a broken instance in
// rotation — so the suite refuses to pass if the failing cases are ever
// deleted.
func assertReadinessCanFail(t *testing.T, cases []healthContractCase) {
	t.Helper()
	var ok, failing int
	for _, tc := range cases {
		if tc.path != "/readyz" {
			continue
		}
		if tc.wantCode == http.StatusOK {
			ok++
			continue
		}
		failing++
	}
	if ok == 0 || failing == 0 {
		t.Fatalf("/readyz coverage is %d passing / %d failing cases; both must be exercised", ok, failing)
	}
}

// assertNoSecretsLeaked checks the one property that matters most about these
// endpoints: they are unauthenticated, so nothing about how this service
// reaches its dependencies may appear in what they return.
func assertNoSecretsLeaked(t *testing.T, path, body string) {
	t.Helper()
	lower := strings.ToLower(body)
	for _, forbidden := range forbiddenInHealthPayload {
		if strings.Contains(lower, strings.ToLower(forbidden)) {
			t.Errorf("GET %s response leaked %q; body: %s", path, forbidden, body)
		}
	}
}

func assertKeySet(t *testing.T, field string, object map[string]json.RawMessage, required, optional []string) {
	t.Helper()
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, key := range required {
		allowed[key] = true
	}
	for _, key := range optional {
		allowed[key] = true
	}

	for _, key := range required {
		if _, present := object[key]; !present {
			t.Errorf("%s is missing required key %q (got %v)", field, key, sortedKeys(object))
		}
	}
	for key := range object {
		if !allowed[key] {
			t.Errorf("%s has unexpected key %q; the response contract is %v", field, key, append(required, optional...))
		}
	}
}

func mustObject(t *testing.T, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("expected a JSON object, got %s: %v", raw, err)
	}
	return object
}

func sortedKeys(object map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// healthDeps builds the production dependency set for one case, with every
// dependency stubbed: the probes are closures over the case's error fields,
// and the Stellar endpoints are local httptest servers.
func (tc healthContractCase) healthDeps(t *testing.T) healthDeps {
	t.Helper()

	horizon := newHorizonStub(t, tc.horizonHealthy)
	soroban := newSorobanStub(t, tc.sorobanHealthy)

	ready := new(atomic.Bool)
	ready.Store(tc.ready)

	deps := healthDeps{
		ready:  ready,
		pingDB: func(context.Context) error { return tc.dbErr },
		poolStats: func() poolStats {
			return poolStats{MaxConns: 25, AcquiredConns: 3, IdleConns: 4, TotalConns: 7}
		},
		probeTimeout: 2 * time.Second,
		httpClient:   &http.Client{Timeout: 2 * time.Second},
		horizonURL:   horizon.URL,
		rpcURL:       soroban.URL,
		startedAt:    time.Now().Add(-90 * time.Second),
		environment:  "test",
		buildVersion: "test-build",
	}
	if !tc.redisNotConfigured {
		deps.pingRedis = func(context.Context) error { return tc.redisErr }
	}
	return deps
}

func newHorizonStub(t *testing.T, healthy bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(upstreamLeakBody))
			return
		}
		_, _ = w.Write([]byte(`{"history_latest_ledger":58493201,"core_latest_ledger":58493201}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newSorobanStub(t *testing.T, healthy bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !healthy {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(upstreamLeakBody))
			return
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"nester-health","result":{"status":"healthy","latestLedger":58493201}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}
