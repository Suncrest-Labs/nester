package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHealthEndpoints_BothPathsReturnOKWhenReady(t *testing.T) {
	var ready atomic.Bool
	ready.Store(true)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", livenessHandler(&ready))
	mux.HandleFunc("GET /healthz", livenessHandler(&ready))

	for _, path := range []string{"/health", "/healthz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		assert.Equalf(t, http.StatusOK, rec.Code, "GET %s should return 200 when ready", path)
		assert.Equalf(t, "ok", rec.Body.String(), "GET %s body", path)
	}
}

func TestLivenessHandler_ReportsDrainingWhenNotReady(t *testing.T) {
	var ready atomic.Bool

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	livenessHandler(&ready)(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "draining", rec.Body.String())
}
