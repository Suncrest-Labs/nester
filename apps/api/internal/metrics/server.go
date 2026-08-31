package metrics

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// Server runs the metrics exposition endpoint on a listener separate from
// the public API.
//
// Separate listener rather than a route on the public router, because an
// open /metrics publishes internal route names, per-route traffic volumes,
// and error rates — a map of the service and a live signal of when it is
// degraded. Gating it behind the existing Authenticate middleware was the
// alternative; a distinct listener is stronger because it does not depend on
// a rule ordering staying correct as the route table grows, and because the
// default bind address (127.0.0.1) makes the endpoint unreachable from off
// the host even if a future ingress rule is written carelessly.
//
// The public mux never learns the /metrics pattern, so a request for it on
// the public port falls through to the API's 404 handling.
type Server struct {
	server *http.Server
	addr   string
	logger *slog.Logger
}

// NewServer builds the internal metrics server bound to addr.
//
// addr should stay on loopback in production unless a scraper needs to reach
// it across the network, in which case bind it to an interface that the
// public ingress does not route to.
func NewServer(addr string, handler http.Handler, logger *slog.Logger) *Server {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", handler)

	// A liveness probe for the metrics listener itself, so an operator can
	// tell "the scrape target is down" apart from "the API is down".
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	return &Server{
		server: &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
		addr:   addr,
		logger: logger,
	}
}

// Start serves the metrics endpoint until Shutdown is called.
//
// It is intended to run in its own goroutine. A failure here is logged and
// not fatal: losing observability should not take down the API.
func (s *Server) Start() {
	s.logger.Info("starting internal metrics listener", "addr", s.addr)

	if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.logger.Error("internal metrics listener stopped", "error", err.Error())
	}
}

// Shutdown gracefully stops the metrics listener.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
