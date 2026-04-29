package middleware

import "net/http"

// CORS returns a middleware that echoes the request's Origin header back in
// Access-Control-Allow-Origin only when the origin is in allowedOrigins.
// Cross-origin responses to other origins carry no CORS headers, so browsers
// block them. Preflight OPTIONS requests short-circuit with 204.
//
// An empty allowedOrigins slice disables cross-origin access entirely: no
// Access-Control-Allow-Origin header is ever emitted.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[o] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Vary ensures caches don't serve a response built for one origin
			// to a request from a different origin.
			w.Header().Set("Vary", "Origin")

			origin := r.Header.Get("Origin")
			if origin != "" {
				if _, ok := allowed[origin]; ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Credentials", "true")

					if r.Method == http.MethodOptions {
						w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
						w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
						w.Header().Set("Access-Control-Max-Age", "86400")
					}
				}
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
