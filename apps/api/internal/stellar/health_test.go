package stellar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestPingSorobanRPC_RequiresHealthyStatus locks in the fix for the
// readiness gate trusting a 200 response with an absent or empty
// result.status. Before the fix, PingSorobanRPC only rejected a
// *non-empty* status other than "healthy", so a proxy or incompatible
// endpoint returning `{"result":{}}` was reported OK.
func TestPingSorobanRPC_RequiresHealthyStatus(t *testing.T) {
	cases := []struct {
		name string
		body string
		ok   bool
	}{
		{
			name: "healthy status reports OK",
			body: `{"jsonrpc":"2.0","id":"nester-health","result":{"status":"healthy","latestLedger":123}}`,
			ok:   true,
		},
		{
			name: "healthy status is case-insensitive",
			body: `{"jsonrpc":"2.0","id":"nester-health","result":{"status":"HEALTHY","latestLedger":123}}`,
			ok:   true,
		},
		{
			name: "empty result object is not OK",
			body: `{"jsonrpc":"2.0","id":"nester-health","result":{}}`,
			ok:   false,
		},
		{
			name: "empty status string is not OK",
			body: `{"jsonrpc":"2.0","id":"nester-health","result":{"status":""}}`,
			ok:   false,
		},
		{
			name: "non-healthy status is not OK",
			body: `{"jsonrpc":"2.0","id":"nester-health","result":{"status":"unhealthy"}}`,
			ok:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			client := &http.Client{Timeout: 2 * time.Second}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			result := PingSorobanRPC(ctx, client, srv.URL)
			if result.OK != tc.ok {
				t.Fatalf("PingSorobanRPC(%q).OK = %v, want %v (result: %+v)", tc.body, result.OK, tc.ok, result)
			}
		})
	}
}
