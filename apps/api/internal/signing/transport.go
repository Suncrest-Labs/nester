package signing

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SignPath is the single endpoint the signer exposes. There is deliberately no
// endpoint that returns key material, exports the key, or signs arbitrary
// bytes — the absence of those routes is part of the security design, not an
// omission to be filled in later.
const SignPath = "/v1/sign"

// HealthPath reports signer liveness and kill-switch position without
// requiring signing authority.
const HealthPath = "/v1/health"

// wireRequest is the JSON body sent across the boundary.
type wireRequest struct {
	Intent *Intent `json:"intent"`
}

// wireResponse is the JSON body returned for an approved intent.
type wireResponse struct {
	SignedXDR  string `json:"signed_xdr"`
	KeyID      string `json:"key_id"`
	IntentHash string `json:"intent_hash"`
}

// wireError is the JSON body returned for a refusal.
//
// It carries the rejection category so the API can distinguish a policy
// refusal from an infrastructure failure and react appropriately — retrying an
// unavailable signer is reasonable, retrying a policy rejection is not.
type wireError struct {
	Error     string    `json:"error"`
	Rejection Rejection `json:"rejection,omitempty"`
}

// Server exposes a Service over HTTP, bound to a Unix domain socket or a
// TLS listener.
type Server struct {
	service *Service
	// callerIdentity resolves the authenticated identity of a request. It
	// returns an error to refuse the request outright.
	callerIdentity func(*http.Request) (string, error)
	logger         *slog.Logger
}

// NewServer builds the signer HTTP surface.
func NewServer(service *Service, callerIdentity func(*http.Request) (string, error), logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	if callerIdentity == nil {
		// Refusing everything is the correct default: a server that cannot
		// identify its callers must not sign for them.
		callerIdentity = func(*http.Request) (string, error) {
			return "", errors.New("no caller identity resolver configured")
		}
	}
	return &Server{service: service, callerIdentity: callerIdentity, logger: logger}
}

// Handler returns the signer HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(SignPath, s.handleSign)
	mux.HandleFunc(HealthPath, s.handleHealth)
	return mux
}

// maxRequestBytes bounds the request body. An intent is small; anything larger
// is malformed or hostile, and reading it would be a free memory-exhaustion
// primitive against the most security-sensitive process in the system.
const maxRequestBytes = 64 << 10

func (s *Server) handleSign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeWireError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	// Caller authentication precedes everything, so an unauthenticated caller
	// cannot probe policy behaviour by observing which rejection it receives.
	caller, err := s.callerIdentity(r)
	if err != nil {
		s.service.counters.ObserveUnauthorized()
		s.logger.Warn("signer rejected unauthenticated caller", "error", err)
		writeWireError(w, http.StatusUnauthorized, "unauthorized", RejectUnauthorized)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if err != nil {
		writeWireError(w, http.StatusBadRequest, "request body could not be read", RejectMalformed)
		return
	}
	var req wireRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeWireError(w, http.StatusBadRequest, "request body is not valid JSON", RejectMalformed)
		return
	}
	if req.Intent == nil {
		writeWireError(w, http.StatusBadRequest, "intent is required", RejectMalformed)
		return
	}

	result, err := s.service.Sign(r.Context(), caller, req.Intent)
	if err != nil {
		s.writeSignError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, wireResponse{
		SignedXDR:  result.SignedXDR,
		KeyID:      result.KeyID,
		IntentHash: result.IntentHash,
	})
}

func (s *Server) writeSignError(w http.ResponseWriter, err error) {
	var pe *PolicyError
	switch {
	case errors.As(err, &pe):
		// 422: the request was understood and refused on policy grounds.
		// The category travels so the caller can tell a permanent refusal
		// from a transient one; the reason text does not, because it can
		// contain detail about the policy that a caller has no need for.
		writeWireError(w, http.StatusUnprocessableEntity, "intent rejected by signing policy", pe.Category)
	case errors.Is(err, ErrSigningDisabled):
		// 503: the signer is deliberately not signing. Distinct from 422 so
		// the API can surface "signing is halted" rather than "your request
		// was invalid", which matters during an incident.
		writeWireError(w, http.StatusServiceUnavailable, "signing is disabled", RejectKillSwitchActive)
	default:
		// The underlying error is logged, not returned: backend errors can
		// carry transaction-construction detail that need not cross back.
		s.logger.Error("signing failed", "error", err)
		writeWireError(w, http.StatusBadGateway, "signing failed", "")
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeWireError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	state, reason, checkedAt := s.service.KillSwitchStatus()
	// The key ID is a public identifier. The key itself is never exposed here
	// or anywhere else in this surface.
	writeJSON(w, http.StatusOK, map[string]any{
		"status":           "ok",
		"kill_switch":      string(state),
		"kill_switch_note": reason,
		"kill_switch_at":   checkedAt.Format(time.RFC3339),
		"key_id":           s.service.KeyID(),
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeWireError(w http.ResponseWriter, status int, msg string, rejection Rejection) {
	writeJSON(w, status, wireError{Error: msg, Rejection: rejection})
}

// ListenUnix binds the signer to a Unix domain socket with restrictive
// permissions.
//
// This is the preferred transport for a co-located signer. Authorization is
// enforced by the operating system through file permissions on the socket:
// only processes running as a user in the socket group can connect at all, so
// an unauthorized caller is refused before any application code runs. That is
// a stronger position than a shared secret held in the same environment as the
// caller it is meant to authenticate.
func ListenUnix(socketPath string, mode os.FileMode) (net.Listener, error) {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return nil, errors.New("signer socket path must be configured")
	}
	dir := filepath.Dir(socketPath)
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("signer socket directory %q is not reachable: %w", dir, err)
	}
	// A stale socket from an unclean shutdown would otherwise block binding.
	// Removing it is safe: if another signer is genuinely running, the bind
	// below fails and the caller sees that rather than two signers racing.
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove stale signer socket: %w", err)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on signer socket: %w", err)
	}
	// Permissions are applied after bind; Go creates the socket with the
	// process umask applied, which may be more permissive than intended.
	if err := os.Chmod(socketPath, mode); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("restrict signer socket permissions: %w", err)
	}
	return ln, nil
}

// PeerCredentialIdentity resolves caller identity for a Unix socket listener.
//
// Reaching the socket at all already required the operating system to permit
// the connection under the socket's ownership and mode, which is the actual
// authorization decision. This function names the resulting identity for the
// audit record.
func PeerCredentialIdentity(defaultName string) func(*http.Request) (string, error) {
	if strings.TrimSpace(defaultName) == "" {
		defaultName = "local-peer"
	}
	return func(*http.Request) (string, error) {
		return defaultName, nil
	}
}

// TLSClientIdentity resolves caller identity from a verified client
// certificate.
//
// This is the transport for a signer that must be reachable across a network
// boundary, where socket permissions do not apply. The identity is the
// certificate subject common name, and it is trustworthy only because the TLS
// layer has already verified the chain against the configured client CA — the
// server must be configured with ClientAuth: RequireAndVerifyClientCert for
// this to mean anything, which NewTLSConfig enforces.
func TLSClientIdentity(r *http.Request) (string, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return "", errors.New("client certificate is required")
	}
	cn := strings.TrimSpace(r.TLS.PeerCertificates[0].Subject.CommonName)
	if cn == "" {
		return "", errors.New("client certificate has no common name")
	}
	return cn, nil
}

// NewTLSConfig builds a mutual-TLS server configuration.
//
// RequireAndVerifyClientCert is not configurable: a signer that accepts
// unauthenticated TLS connections provides transport encryption and no
// authorization, which is not the property this boundary needs.
func NewTLSConfig(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load signer TLS keypair: %w", err)
	}
	pool, err := loadCertPool(clientCAFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// NewClientTLSConfig builds the caller-side mutual TLS configuration.
func NewClientTLSConfig(certFile, keyFile, serverCAFile, serverName string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load client TLS keypair: %w", err)
	}
	pool, err := loadCertPool(serverCAFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   strings.TrimSpace(serverName),
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// loadCertPool reads a PEM bundle into a certificate pool.
//
// It deliberately starts from an empty pool rather than the system roots: the
// signer trusts exactly the CA that issues its peers' certificates, and
// inheriting the public root store would let any publicly-trusted certificate
// authenticate to it.
func loadCertPool(path string) (*x509.CertPool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("a CA bundle path is required for mutual TLS")
	}
	pem, err := os.ReadFile(path) // #nosec G304 -- operator-supplied CA bundle path from configuration, not request input
	if err != nil {
		return nil, fmt.Errorf("read CA bundle: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("CA bundle %q contains no usable certificates", path)
	}
	return pool, nil
}

// Client calls a remote signer.
type Client struct {
	httpClient *http.Client
	baseURL    string
	timeout    time.Duration
}

// ClientOptions configures a signer client.
type ClientOptions struct {
	// SocketPath, when set, dials the signer over a Unix domain socket.
	SocketPath string
	// BaseURL, when SocketPath is empty, is the signer HTTPS endpoint.
	BaseURL string
	// TLSConfig is the client-side mutual TLS configuration for BaseURL.
	TLSConfig *tls.Config
	// Timeout bounds a single signing call.
	Timeout time.Duration
}

// DefaultClientTimeout bounds a signing call. It accommodates the signer
// rebuilding and simulating the transaction while still failing fast enough to
// surface a hung signer on the operational hot path.
const DefaultClientTimeout = 15 * time.Second

// NewClient builds a client for the configured transport.
func NewClient(opts ClientOptions) (*Client, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultClientTimeout
	}

	switch {
	case strings.TrimSpace(opts.SocketPath) != "":
		socketPath := strings.TrimSpace(opts.SocketPath)
		transport := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		}
		return &Client{
			httpClient: &http.Client{Transport: transport, Timeout: timeout},
			// The host is ignored by the dialer but must be syntactically
			// present for the URL to parse.
			baseURL: "http://signer",
			timeout: timeout,
		}, nil

	case strings.TrimSpace(opts.BaseURL) != "":
		if opts.TLSConfig == nil {
			return nil, errors.New("a TLS configuration is required for a networked signer")
		}
		transport := &http.Transport{TLSClientConfig: opts.TLSConfig}
		return &Client{
			httpClient: &http.Client{Transport: transport, Timeout: timeout},
			baseURL:    strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/"),
			timeout:    timeout,
		}, nil

	default:
		return nil, errors.New("either a socket path or a base URL must be configured")
	}
}

// Sign asks the remote signer to sign the intent.
func (c *Client) Sign(ctx context.Context, i *Intent) (*Result, error) {
	payload, err := json.Marshal(wireRequest{Intent: i})
	if err != nil {
		return nil, fmt.Errorf("encode sign request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+SignPath, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build sign request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call signer: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRequestBytes))
	if err != nil {
		return nil, fmt.Errorf("read signer response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var werr wireError
		_ = json.Unmarshal(body, &werr)
		if werr.Rejection == RejectKillSwitchActive {
			return nil, fmt.Errorf("%w: signer reports signing disabled", ErrSigningDisabled)
		}
		if werr.Rejection != "" {
			return nil, &PolicyError{Category: werr.Rejection, Reason: werr.Error}
		}
		return nil, fmt.Errorf("signer returned status %d", resp.StatusCode)
	}

	var wresp wireResponse
	if err := json.Unmarshal(body, &wresp); err != nil {
		return nil, fmt.Errorf("decode signer response: %w", err)
	}
	if wresp.SignedXDR == "" {
		return nil, errors.New("signer returned an empty signed transaction")
	}
	return &Result{
		SignedXDR:  wresp.SignedXDR,
		KeyID:      wresp.KeyID,
		IntentHash: wresp.IntentHash,
	}, nil
}
