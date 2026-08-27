package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// BenchmarkMiddleware measures the per-request cost added to the hot path.
//
// The comparison against BenchmarkBaselineHandler is the number that matters:
// the delta is what every API request pays for instrumentation.
func BenchmarkMiddleware(b *testing.B) {
	m := New()
	mux := newTestMux()
	handler := m.Middleware(mux)(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vaults/"+testUUID, nil)
	recorder := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler.ServeHTTP(recorder, req)
	}
}

func BenchmarkBaselineHandler(b *testing.B) {
	mux := newTestMux()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vaults/"+testUUID, nil)
	recorder := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mux.ServeHTTP(recorder, req)
	}
}

// BenchmarkMiddlewareParallel checks the middleware does not serialise
// concurrent requests. The counters and the gauge are atomic in
// client_golang, so this should scale with GOMAXPROCS.
func BenchmarkMiddlewareParallel(b *testing.B) {
	m := New()
	mux := newTestMux()
	handler := m.Middleware(mux)(mux)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/vaults/"+testUUID, nil)
		recorder := httptest.NewRecorder()
		for pb.Next() {
			handler.ServeHTTP(recorder, req)
		}
	})
}

// BenchmarkResolveRoute isolates the pattern lookup, which is the one piece
// of per-request work this middleware adds beyond two atomic increments and
// a histogram observation.
func BenchmarkResolveRoute(b *testing.B) {
	mux := newTestMux()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/vaults/"+testUUID, nil)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = resolveRoute(mux, req)
	}
}

func BenchmarkStatusClass(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = statusClass(http.StatusOK)
	}
}
