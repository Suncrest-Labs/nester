package middleware

import "net/http"

// SecurityHeaders adds critical security-related HTTP headers to every response.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent browsers from mime-sniffing the response type
		w.Header().Set("X-Content-Type-Options", "nosniff")
		
		// Prevent the page from being rendered in an iframe/frame
		w.Header().Set("X-Frame-Options", "DENY")
		
		// Enforce HTTPS
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		
		// Restrict sources from which content can be loaded
		w.Header().Set("Content-Security-Policy", "default-src 'self'")

		next.ServeHTTP(w, r)
	})
}
