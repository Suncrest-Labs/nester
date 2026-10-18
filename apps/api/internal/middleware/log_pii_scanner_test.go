package middleware

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
)

// Issue #1566: Run the HTTP request logging suite against a JSON log sink and
// assert that no emitted log line matches patterns for JWTs, WebSocket token
// query parameters, Stellar secret seeds (S[A-Z2-7]{55}), email addresses,
// KYC identifiers, or bank account numbers.
func TestHTTPRequestLogScanner_RedactsSecretsAndPII(t *testing.T) {
	// Assemble credential-shaped fixtures dynamically so static secret scanners
	// (gitleaks) do not flag test literals.
	fakeSeed := "S" + "BSVTQO4V6WQNQK4TSFVQVUDCUKYJ2ZQFPKGZVFPWMJXW2WOHVUTPQKZ"
	fakeJWT := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9" + "." +
		"eyJzdWIiOiJ1c2VyLTEyMyIsImVtYWlsIjoidXNlckBuZXN0ZXIuZGV2In0" + "." +
		"dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	fakeEmail := "alice.investor" + "@" + "nester-vault.io"
	fakeIBAN := "DE89" + "370400440532013000"
	fakeAccountNo := "001294857392"
	fakeSSN := "123-" + "45-6789"

	var sink bytes.Buffer
	jsonHandler := slog.NewJSONHandler(&sink, &slog.HandlerOptions{
		Level:       slog.LevelDebug,
		ReplaceAttr: logpkg.RedactReplaceAttr,
	})
	baseLogger := slog.New(jsonHandler)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/verify", func(w http.ResponseWriter, r *http.Request) {
		log := logpkg.FromContext(r.Context())
		log.Info("auth verification attempt",
			"authorization", r.Header.Get("Authorization"),
			"jwt", fakeJWT,
			"operator_seed", fakeSeed,
		)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		log := logpkg.FromContext(r.Context())
		log.Warn("websocket handshake failed",
			"raw_url", r.URL.String(),
			"token", r.URL.Query().Get("token"),
			"error", errors.New("invalid ws token at /ws?token="+fakeJWT+" for seed "+fakeSeed),
		)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/api/v1/users/kyc", func(w http.ResponseWriter, r *http.Request) {
		log := logpkg.FromContext(r.Context())
		log.Error("kyc payout validation error",
			"email", fakeEmail,
			"ssn", fakeSSN,
			"iban", fakeIBAN,
			"account_number", fakeAccountNo,
			"error", errors.New("payout rejected for "+fakeEmail+" account_number="+fakeAccountNo+" iban="+fakeIBAN+" ssn="+fakeSSN),
		)
		w.WriteHeader(http.StatusUnprocessableEntity)
	})

	loggedRouter := Logging(baseLogger)(mux)

	requests := []*http.Request{
		func() *http.Request {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify", strings.NewReader(`{"token":"`+fakeJWT+`"}`))
			req.Header.Set("Authorization", "Bearer "+fakeJWT)
			return req
		}(),
		httptest.NewRequest(http.MethodGet, "/ws?token="+fakeJWT+"&client=web", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/users/kyc?email="+fakeEmail, nil),
	}

	for _, req := range requests {
		rec := httptest.NewRecorder()
		loggedRouter.ServeHTTP(rec, req)
	}

	logOutput := sink.String()
	if strings.TrimSpace(logOutput) == "" {
		t.Fatal("expected JSON log sink to capture request logs, got empty output")
	}

	// Scanner patterns required by #1566 acceptance criteria:
	scannerPatterns := []struct {
		name    string
		pattern *regexp.Regexp
	}{
		{
			name:    "JWT token",
			pattern: regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`),
		},
		{
			name:    "Stellar secret seed (S[A-Z2-7]{55})",
			pattern: regexp.MustCompile(`S[A-Z2-7]{55}`),
		},
		{
			name:    "Email address",
			pattern: regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`),
		},
		{
			name:    "WebSocket token query parameter",
			pattern: regexp.MustCompile(`(?i)[?&](?:token|access_token)=[^&\s"\[]+`),
		},
		{
			name:    "IBAN account number",
			pattern: regexp.MustCompile(`\b[A-Z]{2}\d{2}[A-Z0-9]{11,30}\b`),
		},
		{
			name:    "US SSN",
			pattern: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
		},
	}

	lines := strings.Split(strings.TrimSpace(logOutput), "\n")
	for i, line := range lines {
		for _, rule := range scannerPatterns {
			if match := rule.pattern.FindString(line); match != "" {
				t.Errorf("log line %d leaked %s (%q): %s", i+1, rule.name, match, line)
			}
		}
		for _, literal := range []string{fakeSeed, fakeJWT, fakeEmail, fakeIBAN, fakeAccountNo, fakeSSN} {
			if strings.Contains(line, literal) {
				t.Errorf("log line %d leaked raw sensitive literal %q: %s", i+1, literal, line)
			}
		}
	}
}
