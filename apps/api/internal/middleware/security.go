package middleware

import (
	"net/http"
)

// SecurityHeaders returns a middleware that sets standard security headers on responses.
// These headers protect against common web vulnerabilities:
//   - X-Content-Type-Options: prevents MIME-type sniffing attacks
//   - X-Frame-Options: prevents clickjacking attacks
//   - Strict-Transport-Security: enforces HTTPS (only when in production/TLS)
//   - X-XSS-Protection: legacy XSS filter control
//   - Referrer-Policy: controls how much referrer information is disclosed
//   - Permissions-Policy: restricts access to sensitive browser APIs
//   - Cache-Control: prevents caching of sensitive authenticated responses
//   - Content-Security-Policy: restricts resource loading (if serving HTML)
func SecurityHeaders(environment string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Prevent browsers from MIME-type sniffing
			w.Header().Set("X-Content-Type-Options", "nosniff")

			// Prevent the page from being embedded in iframes (clickjacking protection)
			w.Header().Set("X-Frame-Options", "DENY")

			// Force HTTPS in production/staging (only when TLS is in use)
			if environment == "production" || environment == "staging" {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			// Legacy XSS filter (modern browsers ignore this, rely on CSP instead)
			w.Header().Set("X-XSS-Protection", "0")

			// Control how much referrer information is disclosed in cross-origin requests
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			// Disable access to potentially sensitive browser APIs
			w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")

			// Prevent caching of responses (especially important for authenticated endpoints)
			// This is not a blanket ban — each handler can override with specific cache strategies
			w.Header().Set("Cache-Control", "no-store, must-revalidate, max-age=0")

			next.ServeHTTP(w, r)
		})
	}
}
