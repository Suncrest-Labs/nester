package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// v1 and v2 of the same endpoint return different shapes — the mapping
// lives at the transport edge; both handlers would call the same service in
// production.
func v1VaultHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"vault_name": "alpha", "apy": "5.1"})
	})
}

func v2VaultHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"vault": map[string]any{"name": "alpha", "apy_bps": 510},
		})
	})
}

func newTestAPI(t *testing.T) (*VersionedAPI, *CounterUsage) {
	t.Helper()
	usage := NewCounterUsage()
	api := NewVersionedAPI("v1", usage)

	if err := api.Register(VersionState{Name: "v1", Deprecated: true, Sunset: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), Successor: "v2"}); err != nil {
		t.Fatal(err)
	}
	if err := api.Register(VersionState{Name: "v2"}); err != nil {
		t.Fatal(err)
	}
	if err := api.Register(VersionState{Name: "v0", Retired: true, Successor: "v2"}); err != nil {
		t.Fatal(err)
	}

	if err := api.Handle("v1", "GET /vaults", v1VaultHandler()); err != nil {
		t.Fatal(err)
	}
	if err := api.Handle("v2", "GET /vaults", v2VaultHandler()); err != nil {
		t.Fatal(err)
	}
	if err := api.Handle("v0", "GET /vaults", v1VaultHandler()); err != nil {
		t.Fatal(err)
	}
	return api, usage
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// TestVersionsCoexistWithTheirOwnContracts: v1 and v2 of the same endpoint
// serve simultaneously, each returning its own response shape.
func TestVersionsCoexistWithTheirOwnContracts(t *testing.T) {
	api, _ := newTestAPI(t)
	h := api.Handler()

	res1 := get(t, h, "/v1/vaults")
	if res1.Code != http.StatusOK {
		t.Fatalf("v1 status = %d", res1.Code)
	}
	var v1 map[string]any
	if err := json.Unmarshal(res1.Body.Bytes(), &v1); err != nil {
		t.Fatal(err)
	}
	if _, ok := v1["vault_name"]; !ok {
		t.Fatalf("v1 contract missing vault_name: %v", v1)
	}

	res2 := get(t, h, "/v2/vaults")
	if res2.Code != http.StatusOK {
		t.Fatalf("v2 status = %d", res2.Code)
	}
	var v2 map[string]any
	if err := json.Unmarshal(res2.Body.Bytes(), &v2); err != nil {
		t.Fatal(err)
	}
	if _, ok := v2["vault"]; !ok {
		t.Fatalf("v2 contract missing nested vault: %v", v2)
	}
	if _, leaked := v2["vault_name"]; leaked {
		t.Fatal("v2 must not leak the v1 shape")
	}
}

// TestDeprecatedVersionEmitsHeaders: every response from a deprecated
// version carries Deprecation and Sunset headers plus a successor Link.
func TestDeprecatedVersionEmitsHeaders(t *testing.T) {
	api, _ := newTestAPI(t)
	res := get(t, api.Handler(), "/v1/vaults")

	if res.Header().Get("Deprecation") != "true" {
		t.Fatal("deprecated version must send Deprecation header on every response")
	}
	sunset := res.Header().Get("Sunset")
	if sunset == "" {
		t.Fatal("deprecated version must send Sunset header")
	}
	if _, err := time.Parse(http.TimeFormat, sunset); err != nil {
		t.Fatalf("Sunset %q is not an HTTP date: %v", sunset, err)
	}
	if got := res.Header().Get("Link"); got != `</v2>; rel="successor-version"` {
		t.Fatalf("Link = %q", got)
	}

	// The current version carries none of this noise.
	res2 := get(t, api.Handler(), "/v2/vaults")
	if res2.Header().Get("Deprecation") != "" || res2.Header().Get("Sunset") != "" {
		t.Fatal("current version must not send deprecation headers")
	}
}

// TestRetiredVersionReturns410WithGuidance: a retired version is a clear,
// documented 410 Gone pointing at the current version — never a silent 404
// that looks like a bug.
func TestRetiredVersionReturns410WithGuidance(t *testing.T) {
	api, _ := newTestAPI(t)
	res := get(t, api.Handler(), "/v0/vaults")

	if res.Code != http.StatusGone {
		t.Fatalf("retired version status = %d, want 410", res.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["current_version"] != "v2" {
		t.Fatalf("410 body must point at the current version, got %v", body)
	}
}

// TestUnversionedRequestsRouteToPinnedDefault: no version in the path means
// the pinned default (v1 here) — stable behaviour, not "latest" (v2).
func TestUnversionedRequestsRouteToPinnedDefault(t *testing.T) {
	api, _ := newTestAPI(t)
	res := get(t, api.Handler(), "/vaults")

	if res.Code != http.StatusOK {
		t.Fatalf("default route status = %d", res.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["vault_name"]; !ok {
		t.Fatalf("default must serve the pinned v1 contract, not latest; got %v", body)
	}
}

// TestPerVersionUsageIsRecorded: retirement decisions are data-driven —
// hits are counted per version.
func TestPerVersionUsageIsRecorded(t *testing.T) {
	api, usage := newTestAPI(t)
	h := api.Handler()

	get(t, h, "/v1/vaults")
	get(t, h, "/v1/vaults")
	get(t, h, "/v2/vaults")
	get(t, h, "/v0/vaults") // even retired hits are counted: they identify stragglers

	snap := usage.Snapshot()
	if snap["v1"] != 2 || snap["v2"] != 1 || snap["v0"] != 1 {
		t.Fatalf("usage snapshot = %v, want v1:2 v2:1 v0:1", snap)
	}
}

// TestRegisterValidation: malformed lifecycle declarations are rejected.
func TestRegisterValidation(t *testing.T) {
	api := NewVersionedAPI("v1", nil)
	if err := api.Register(VersionState{Name: "one"}); err == nil {
		t.Fatal("non-vN name must be rejected")
	}
	if err := api.Register(VersionState{Name: "v3", Retired: true}); err == nil {
		t.Fatal("retired version without successor must be rejected")
	}
	if err := api.Handle("v9", "GET /x", http.NotFoundHandler()); err == nil {
		t.Fatal("mounting on an unregistered version must fail")
	}
}
