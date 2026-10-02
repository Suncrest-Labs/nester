package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/systemstate"
)

type fakeSystemStateRepo struct {
	value string
	err   error
}

func (f *fakeSystemStateRepo) Get(_ context.Context, _ string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.value, nil
}

func (f *fakeSystemStateRepo) Set(_ context.Context, _ string, value string) error {
	f.value = value
	return nil
}

func newTestGate(mode string) *MaintenanceGate {
	repo := &fakeSystemStateRepo{value: mode}
	// Interval is irrelevant here: NewMaintenanceGate does a synchronous
	// refresh before returning, and the test doesn't outlive one process.
	return NewMaintenanceGate(repo, time.Hour)
}

var maintenanceRules = []RouteRule{
	{PathPrefix: "/health", Public: true},
}

func TestMaintenanceOffAllowsEverything(t *testing.T) {
	gate := newTestGate(systemstate.ModeOff)
	handler := gate.Middleware(maintenanceRules)(ok200)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deposit", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 in normal mode", rec.Code)
	}
}

func TestMaintenanceHaltBlocksProtectedRoutes(t *testing.T) {
	gate := newTestGate(systemstate.ModeHalt)
	handler := gate.Middleware(maintenanceRules)(ok200)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vaults", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503 in halt mode", rec.Code)
	}
}

func TestMaintenanceHaltAllowsPublicRoutes(t *testing.T) {
	gate := newTestGate(systemstate.ModeHalt)
	handler := gate.Middleware(maintenanceRules)(ok200)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 for public route even in halt mode", rec.Code)
	}
}

func TestMaintenanceReadOnlyBlocksMutations(t *testing.T) {
	gate := newTestGate(systemstate.ModeReadOnly)
	handler := gate.Middleware(maintenanceRules)(ok200)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/v1/vaults", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: got %d, want 503 in read-only mode", method, rec.Code)
		}
	}
}

func TestMaintenanceReadOnlyAllowsGET(t *testing.T) {
	gate := newTestGate(systemstate.ModeReadOnly)
	handler := gate.Middleware(maintenanceRules)(ok200)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vaults", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 for GET in read-only mode", rec.Code)
	}
}
