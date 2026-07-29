package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// API versioning and deprecation framework (issue #842).
//
// Scheme: URL-path versioning (/v1/..., /v2/...) applied uniformly — the
// most explicit and cache-friendly option. Version-specific code is confined
// to the transport edge (handlers/DTOs); shared business logic stays in the
// services and is never forked per version.
//
// Lifecycle: active → deprecated (Deprecation + Sunset headers on every
// response) → retired (410 Gone with a pointer to the current version,
// never a silent 404). Per-version usage is counted so retirement decisions
// are data-driven.
//
// Default: requests without a version prefix route to a PINNED default —
// not "latest", because "latest" silently changes behaviour under consumers.
// The default changes only deliberately, with a code change.

// VersionState describes one API version's lifecycle position.
type VersionState struct {
	// Name is the path prefix segment, e.g. "v1".
	Name string
	// Deprecated marks the version as on its way out; responses carry
	// Deprecation and Sunset headers.
	Deprecated bool
	// Sunset is the announced retirement date, emitted in the Sunset
	// header (RFC 8594) while deprecated.
	Sunset time.Time
	// Retired versions return 410 Gone with a pointer to Successor.
	Retired bool
	// Successor is the version consumers should move to, e.g. "v2".
	Successor string
}

// UsageRecorder counts requests per version so the team knows who is still
// on old versions before retiring them. Wire the metrics registry here.
type UsageRecorder interface {
	RecordVersionHit(version string)
}

// CounterUsage is an in-memory UsageRecorder exposing a snapshot for
// metrics scraping and tests.
type CounterUsage struct {
	mu     sync.Mutex
	counts map[string]int64
}

// NewCounterUsage builds an empty counter.
func NewCounterUsage() *CounterUsage {
	return &CounterUsage{counts: make(map[string]int64)}
}

// RecordVersionHit implements UsageRecorder.
func (c *CounterUsage) RecordVersionHit(version string) {
	c.mu.Lock()
	c.counts[version]++
	c.mu.Unlock()
}

// Snapshot returns a copy of the per-version hit counts.
func (c *CounterUsage) Snapshot() map[string]int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]int64, len(c.counts))
	for k, v := range c.counts {
		out[k] = v
	}
	return out
}

// VersionedAPI mounts versioned route groups and enforces the lifecycle
// rules uniformly across every endpoint.
type VersionedAPI struct {
	mux      *http.ServeMux
	versions map[string]*VersionState
	// defaultVersion is the pinned version unprefixed requests route to.
	defaultVersion string
	usage          UsageRecorder
}

// NewVersionedAPI creates the version router. defaultVersion must be
// registered before Handler is used; it is pinned, documented behaviour —
// never implicitly "latest".
func NewVersionedAPI(defaultVersion string, usage UsageRecorder) *VersionedAPI {
	if usage == nil {
		usage = NewCounterUsage()
	}
	return &VersionedAPI{
		mux:            http.NewServeMux(),
		versions:       make(map[string]*VersionState),
		defaultVersion: defaultVersion,
		usage:          usage,
	}
}

// Register declares a version's lifecycle state. Must be called before
// mounting routes for that version.
func (v *VersionedAPI) Register(state VersionState) error {
	if state.Name == "" || !strings.HasPrefix(state.Name, "v") {
		return fmt.Errorf("server: version name %q must look like v1, v2, ...", state.Name)
	}
	if state.Retired && state.Successor == "" {
		return fmt.Errorf("server: retired version %q must name a successor", state.Name)
	}
	v.versions[state.Name] = &state
	return nil
}

// Handle mounts handler for pattern under a registered version, e.g.
// Handle("v1", "GET /vaults", h) serves "GET /v1/vaults". The handler is
// wrapped with the version's lifecycle middleware: usage counting,
// deprecation headers, and 410 short-circuit when retired.
func (v *VersionedAPI) Handle(version, pattern string, handler http.Handler) error {
	state, ok := v.versions[version]
	if !ok {
		return fmt.Errorf("server: version %q not registered", version)
	}

	method, path, found := strings.Cut(pattern, " ")
	if !found {
		// Pattern without a method: treat the whole thing as a path.
		method, path = "", pattern
	}

	versionedPath := "/" + version + path
	full := versionedPath
	if method != "" {
		full = method + " " + versionedPath
	}
	v.mux.Handle(full, v.lifecycle(state, handler))

	// The pinned default version is also reachable unprefixed.
	if version == v.defaultVersion {
		unprefixed := path
		if method != "" {
			unprefixed = method + " " + path
		}
		v.mux.Handle(unprefixed, v.lifecycle(state, handler))
	}
	return nil
}

// Handler returns the assembled router.
func (v *VersionedAPI) Handler() http.Handler {
	return v.mux
}

// lifecycle wraps a versioned handler with the uniform version rules.
func (v *VersionedAPI) lifecycle(state *VersionState, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.usage.RecordVersionHit(state.Name)

		if state.Retired {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusGone)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":           fmt.Sprintf("API version %s has been retired", state.Name),
				"current_version": state.Successor,
				"hint":            fmt.Sprintf("use /%s%s", state.Successor, strings.TrimPrefix(r.URL.Path, "/"+state.Name)),
			})
			return
		}

		if state.Deprecated {
			// RFC 8594-style signalling on EVERY response so a consumer
			// polling the API learns it is on a dying version without
			// reading a changelog.
			w.Header().Set("Deprecation", "true")
			if !state.Sunset.IsZero() {
				w.Header().Set("Sunset", state.Sunset.UTC().Format(http.TimeFormat))
			}
			if state.Successor != "" {
				w.Header().Set("Link", fmt.Sprintf("</%s>; rel=\"successor-version\"", state.Successor))
			}
		}

		next.ServeHTTP(w, r)
	})
}
