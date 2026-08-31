// Command signer runs the isolated transaction-signing service.
//
// This process holds the Stellar operator secret. The API process does not.
// It exposes exactly one signing endpoint, which accepts a typed transaction
// intent, validates it against policy, and returns a signed envelope — never
// key material, and never a signature over caller-supplied bytes.
//
// Deployment: see docs/security/signing-isolation.md. The short version is that
// this runs as its own container with the operator secret in its environment
// and nothing else, sharing only a socket volume with the API.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/signing"
	"github.com/suncrestlabs/nester/apps/api/internal/stellar"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		// The error is logged rather than printed with the secret-bearing
		// environment in scope. Startup failures here are configuration
		// problems and must be loud.
		logger.Error("signer failed to start", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	backend, err := stellar.NewSigningBackend(cfg.rpcURL, cfg.horizonURL, cfg.networkPassphrase, cfg.operatorSecret)
	if err != nil {
		return fmt.Errorf("build signing backend: %w", err)
	}

	killSwitch, err := signing.NewKillSwitch(cfg.killSwitchPath)
	if err != nil {
		return fmt.Errorf("configure kill switch: %w", err)
	}

	policy := signing.NewPolicy(
		cfg.networkPassphrase,
		cfg.allowedContracts,
		cfg.allowedOperations,
		cfg.maxAmountStroops,
		cfg.maxIntentAge,
		cfg.clockSkew,
	)

	service, err := signing.NewService(signing.ServiceOptions{
		Backend:    backend,
		Policy:     policy,
		KillSwitch: killSwitch,
		Sink:       signing.NewSlogSink(logger),
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("build signing service: %w", err)
	}

	// Evaluate the switch once at startup so its position is visible in the
	// logs from the outset rather than discovered on the first signing attempt.
	state, reason, _ := func() (signing.State, string, time.Time) {
		_, _ = killSwitch.Check()
		return killSwitch.Status()
	}()

	logger.Info("signer starting",
		"key_id", backend.KeyID(),
		"network", signing.NetworkLabel(cfg.networkPassphrase),
		"allowed_contracts", len(cfg.allowedContracts),
		"allowed_operations", len(cfg.allowedOperations),
		"max_amount_stroops", cfg.maxAmountStroops,
		"max_intent_age", cfg.maxIntentAge.String(),
		"kill_switch_path", killSwitch.Path(),
		"kill_switch_state", string(state),
		"kill_switch_note", reason,
	)

	listener, identity, err := buildListener(cfg, logger)
	if err != nil {
		return err
	}

	server := signing.NewServer(service, identity, logger)
	httpServer := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if serveErr := httpServer.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("signer shutting down")
	case serveErr := <-errCh:
		return fmt.Errorf("signer server: %w", serveErr)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("signer shutdown: %w", err)
	}
	return nil
}

func buildListener(cfg *config, logger *slog.Logger) (net.Listener, func(*http.Request) (string, error), error) {
	// A Unix socket is preferred: authorization is enforced by the operating
	// system before any application code runs.
	if cfg.socketPath != "" {
		ln, err := signing.ListenUnix(cfg.socketPath, cfg.socketMode)
		if err != nil {
			return nil, nil, err
		}
		logger.Info("signer listening on unix socket",
			"path", cfg.socketPath, "mode", fmt.Sprintf("%#o", cfg.socketMode))
		return ln, signing.PeerCredentialIdentity(cfg.peerName), nil
	}

	// Otherwise mutual TLS, for a signer reachable across a network boundary.
	tlsCfg, err := signing.NewTLSConfig(cfg.tlsCertFile, cfg.tlsKeyFile, cfg.tlsClientCAFile)
	if err != nil {
		return nil, nil, err
	}
	ln, err := net.Listen("tcp", cfg.listenAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("listen on %s: %w", cfg.listenAddr, err)
	}
	logger.Info("signer listening with mutual TLS", "addr", cfg.listenAddr)
	return tlsListener(ln, tlsCfg), signing.TLSClientIdentity, nil
}

type config struct {
	rpcURL            string
	horizonURL        string
	networkPassphrase string
	operatorSecret    string

	socketPath string
	socketMode os.FileMode
	peerName   string

	listenAddr      string
	tlsCertFile     string
	tlsKeyFile      string
	tlsClientCAFile string

	killSwitchPath    string
	allowedContracts  []string
	allowedOperations []signing.Operation
	maxAmountStroops  int64
	maxIntentAge      time.Duration
	clockSkew         time.Duration
}

func loadConfig() (*config, error) {
	cfg := &config{
		rpcURL:            os.Getenv("STELLAR_RPC_URL"),
		horizonURL:        os.Getenv("STELLAR_HORIZON_URL"),
		networkPassphrase: os.Getenv("STELLAR_NETWORK_PASSPHRASE"),
		operatorSecret:    os.Getenv("STELLAR_OPERATOR_SECRET"),
		socketPath:        strings.TrimSpace(os.Getenv("SIGNER_SOCKET_PATH")),
		peerName:          strings.TrimSpace(os.Getenv("SIGNER_PEER_NAME")),
		listenAddr:        strings.TrimSpace(os.Getenv("SIGNER_LISTEN_ADDR")),
		tlsCertFile:       strings.TrimSpace(os.Getenv("SIGNER_TLS_CERT_FILE")),
		tlsKeyFile:        strings.TrimSpace(os.Getenv("SIGNER_TLS_KEY_FILE")),
		tlsClientCAFile:   strings.TrimSpace(os.Getenv("SIGNER_TLS_CLIENT_CA_FILE")),
		killSwitchPath:    strings.TrimSpace(os.Getenv("SIGNER_KILL_SWITCH_PATH")),
	}

	for _, required := range []struct {
		name  string
		value string
	}{
		{"STELLAR_RPC_URL", cfg.rpcURL},
		{"STELLAR_HORIZON_URL", cfg.horizonURL},
		{"STELLAR_NETWORK_PASSPHRASE", cfg.networkPassphrase},
		{"STELLAR_OPERATOR_SECRET", cfg.operatorSecret},
		{"SIGNER_KILL_SWITCH_PATH", cfg.killSwitchPath},
	} {
		if strings.TrimSpace(required.value) == "" {
			// The value is never echoed — only the variable name.
			return nil, fmt.Errorf("%s is required", required.name)
		}
	}

	if cfg.socketPath == "" && cfg.listenAddr == "" {
		return nil, errors.New("either SIGNER_SOCKET_PATH or SIGNER_LISTEN_ADDR must be set")
	}

	mode, err := parseFileMode(os.Getenv("SIGNER_SOCKET_MODE"), 0o660)
	if err != nil {
		return nil, err
	}
	cfg.socketMode = mode

	// The contract allowlist is mandatory and has no default. A signer that
	// defaulted to "any contract" would be a signing oracle constrained only by
	// function name, which is not the property this design promises.
	cfg.allowedContracts = splitList(os.Getenv("SIGNER_ALLOWED_CONTRACTS"))
	if len(cfg.allowedContracts) == 0 {
		return nil, errors.New("SIGNER_ALLOWED_CONTRACTS must list at least one contract address")
	}

	ops := splitList(os.Getenv("SIGNER_ALLOWED_OPERATIONS"))
	if len(ops) == 0 {
		// Defaulting to every operation the signer knows how to build is safe
		// only because that set is itself closed and small. It is still logged
		// at startup so the effective policy is visible.
		cfg.allowedOperations = signing.KnownOperations()
	} else {
		for _, raw := range ops {
			op := signing.Operation(raw)
			if _, known := signing.ShapeFor(op); !known {
				return nil, fmt.Errorf("SIGNER_ALLOWED_OPERATIONS contains unknown operation %q", raw)
			}
			cfg.allowedOperations = append(cfg.allowedOperations, op)
		}
	}

	cfg.maxAmountStroops, err = parseInt64(os.Getenv("SIGNER_MAX_AMOUNT_STROOPS"), 0)
	if err != nil {
		return nil, fmt.Errorf("SIGNER_MAX_AMOUNT_STROOPS: %w", err)
	}
	if cfg.maxAmountStroops < 0 {
		return nil, errors.New("SIGNER_MAX_AMOUNT_STROOPS must not be negative")
	}

	cfg.maxIntentAge, err = parseDuration(os.Getenv("SIGNER_MAX_INTENT_AGE"), signing.DefaultMaxIntentAge)
	if err != nil {
		return nil, fmt.Errorf("SIGNER_MAX_INTENT_AGE: %w", err)
	}
	cfg.clockSkew, err = parseDuration(os.Getenv("SIGNER_CLOCK_SKEW"), signing.DefaultClockSkew)
	if err != nil {
		return nil, fmt.Errorf("SIGNER_CLOCK_SKEW: %w", err)
	}

	return cfg, nil
}

func splitList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseInt64(raw string, def int64) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}

func parseDuration(raw string, def time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	return time.ParseDuration(raw)
}

func parseFileMode(raw string, def os.FileMode) (os.FileMode, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	v, err := strconv.ParseUint(raw, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("SIGNER_SOCKET_MODE must be an octal mode: %w", err)
	}
	// Reject a world-writable socket outright. It would let any local process
	// request signatures, which defeats the transport's entire authorization
	// model.
	mode := os.FileMode(v)
	if mode&0o002 != 0 {
		return 0, errors.New("SIGNER_SOCKET_MODE must not be world-writable")
	}
	return mode, nil
}

// tlsListener wraps a TCP listener in the signer's mutual-TLS configuration.
func tlsListener(inner net.Listener, cfg *tls.Config) net.Listener {
	return tls.NewListener(inner, cfg)
}
