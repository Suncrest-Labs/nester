package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/systemstate"
)

// mutatingMethods are blocked in ModeReadOnly.
var mutatingMethods = map[string]bool{
	http.MethodPost:   true,
	http.MethodPut:    true,
	http.MethodPatch:  true,
	http.MethodDelete: true,
}

// MaintenanceGate polls the system_state maintenance.mode flag and enforces it
// on every request: ModeHalt rejects everything except public routes;
// ModeReadOnly rejects mutating methods (POST/PUT/PATCH/DELETE) but lets GET
// requests through, for lower-impact maintenance windows.
type MaintenanceGate struct {
	repo systemstate.Repository
	mode atomic.Value // string
}

// NewMaintenanceGate constructs a gate and starts polling repo for the current
// mode every interval. Call Stop to end polling.
func NewMaintenanceGate(repo systemstate.Repository, interval time.Duration) *MaintenanceGate {
	g := &MaintenanceGate{repo: repo}
	g.mode.Store(systemstate.ModeOff)
	g.refresh(context.Background())
	go g.pollLoop(interval)
	return g
}

func (g *MaintenanceGate) pollLoop(interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		g.refresh(context.Background())
	}
}

func (g *MaintenanceGate) refresh(ctx context.Context) {
	value, err := g.repo.Get(ctx, systemstate.KeyMaintenanceMode)
	if err != nil {
		return
	}
	g.mode.Store(value)
}

// Mode returns the currently cached maintenance mode.
func (g *MaintenanceGate) Mode() string {
	return g.mode.Load().(string)
}

// Middleware rejects requests according to the current mode. rules are the
// same RouteRule list passed to Authenticate; a route marked Public is never
// blocked (health checks, auth handshake, etc. must keep working).
func (g *MaintenanceGate) Middleware(rules []RouteRule) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rule := matchRule(rules, r); rule != nil && rule.Public {
				next.ServeHTTP(w, r)
				return
			}

			switch g.Mode() {
			case systemstate.ModeHalt:
				writeMaintenanceError(w, "service is in maintenance halt mode")
				return
			case systemstate.ModeReadOnly:
				if mutatingMethods[r.Method] {
					writeMaintenanceError(w, "service is in read-only maintenance mode")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeMaintenanceError(w http.ResponseWriter, msg string) {
	type errBody struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	type envelope struct {
		Success bool    `json:"success"`
		Error   errBody `json:"error"`
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(envelope{Success: false, Error: errBody{Code: http.StatusServiceUnavailable, Message: msg}})
}
