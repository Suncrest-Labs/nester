package middleware

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// AbuseAction is deliberately graduated: suspicious clients are challenged,
// never irrecoverably blocked by this layer.
type AbuseAction string

const (
	AbuseAllow     AbuseAction = "allow"
	AbuseChallenge AbuseAction = "challenge"
	maxAbuseKeyLen             = 256
)

// AbuseEvent is shared with fraud/anomaly consumers and platform metrics.
type AbuseEvent struct {
	Endpoint    string
	Kind        string
	Fingerprint string
	Action      AbuseAction
	At          time.Time
}

type AbuseObserver interface {
	ObserveAbuse(AbuseEvent)
}

type AbuseConfig struct {
	Window            time.Duration
	EscalationTTL     time.Duration
	GlobalFailures    int
	EnumerationProbes int
	BotVelocity       int
}

type abuseWindow struct {
	start        time.Time
	failures     int
	probes       map[string]struct{}
	fingerprints map[string]int
	escalatedTil time.Time
}

// AbuseProtector detects aggregate patterns that remain invisible to per-key
// rate limits. State can be backed by Redis through a production adapter; this
// in-memory implementation keeps the policy deterministic and testable.
type AbuseProtector struct {
	mu       sync.Mutex
	cfg      AbuseConfig
	now      func() time.Time
	windows  map[string]*abuseWindow
	observer AbuseObserver
}

func NewAbuseProtector(cfg AbuseConfig, observer AbuseObserver) *AbuseProtector {
	return &AbuseProtector{
		cfg: cfg, now: time.Now, windows: make(map[string]*abuseWindow), observer: observer,
	}
}

func (p *AbuseProtector) window(endpoint string) *abuseWindow {
	now := p.now()
	w := p.windows[endpoint]
	if w == nil || now.Sub(w.start) >= p.cfg.Window {
		var escalation time.Time
		if w != nil {
			escalation = w.escalatedTil
		}
		w = &abuseWindow{
			start: now, probes: make(map[string]struct{}), fingerprints: make(map[string]int),
			escalatedTil: escalation,
		}
		p.windows[endpoint] = w
	}
	return w
}

// RecordFailedAuth aggregates failures across IPs. Crossing the threshold
// temporarily requires step-up verification for the targeted endpoint.
func (p *AbuseProtector) RecordFailedAuth(endpoint, fingerprint string) AbuseAction {
	p.mu.Lock()
	fingerprint = boundedAbuseKey(fingerprint)
	w := p.window(endpoint)
	w.failures++
	if _, exists := w.fingerprints[fingerprint]; exists ||
		len(w.fingerprints) < p.cfg.GlobalFailures {
		w.fingerprints[fingerprint]++
	}
	if w.failures >= p.cfg.GlobalFailures {
		w.escalatedTil = p.now().Add(p.cfg.EscalationTTL)
		return p.finish(endpoint, "credential_stuffing", fingerprint, AbuseChallenge)
	}
	return p.finish(endpoint, "failed_auth", fingerprint, AbuseAllow)
}

// RecordProbe detects enumeration by distinct resource identifiers.
func (p *AbuseProtector) RecordProbe(endpoint, resource, fingerprint string) AbuseAction {
	p.mu.Lock()
	resource, fingerprint = boundedAbuseKey(resource), boundedAbuseKey(fingerprint)
	w := p.window(endpoint)
	if len(w.probes) < p.cfg.EnumerationProbes {
		w.probes[resource] = struct{}{}
	}
	if len(w.probes) >= p.cfg.EnumerationProbes {
		w.escalatedTil = p.now().Add(p.cfg.EscalationTTL)
		return p.finish(endpoint, "enumeration", fingerprint, AbuseChallenge)
	}
	return p.finish(endpoint, "lookup", fingerprint, AbuseAllow)
}

// RecordSensitiveFlow applies a challenge to high-velocity, shared behavioral
// fingerprints while normal signup/auth/reward flows continue unchanged.
func (p *AbuseProtector) RecordSensitiveFlow(endpoint, fingerprint string) AbuseAction {
	p.mu.Lock()
	fingerprint = boundedAbuseKey(fingerprint)
	w := p.window(endpoint)
	if _, exists := w.fingerprints[fingerprint]; exists ||
		len(w.fingerprints) < p.cfg.BotVelocity {
		w.fingerprints[fingerprint]++
	}
	if w.fingerprints[fingerprint] >= p.cfg.BotVelocity {
		w.escalatedTil = p.now().Add(p.cfg.EscalationTTL)
		return p.finish(endpoint, "bot_velocity", fingerprint, AbuseChallenge)
	}
	if p.now().Before(w.escalatedTil) {
		return p.finish(endpoint, "adaptive_step_up", fingerprint, AbuseChallenge)
	}
	return p.finish(endpoint, "normal", fingerprint, AbuseAllow)
}

func boundedAbuseKey(value string) string {
	if len(value) > maxAbuseKeyLen {
		return value[:maxAbuseKeyLen]
	}
	return value
}

// finish snapshots the event and releases state before invoking application
// observers, which may perform I/O or re-enter the protector.
func (p *AbuseProtector) finish(endpoint, kind, fingerprint string, action AbuseAction) AbuseAction {
	event := AbuseEvent{
		Endpoint: endpoint, Kind: kind, Fingerprint: fingerprint, Action: action, At: p.now(),
	}
	observer := p.observer
	p.mu.Unlock()
	if observer != nil {
		observer.ObserveAbuse(event)
	}
	return action
}

// ChallengeMiddleware provides a recoverable step-up path instead of a block.
func ChallengeMiddleware(decide func(*http.Request) AbuseAction) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if decide(r) == AbuseChallenge {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Abuse-Action", "challenge")
				w.WriteHeader(http.StatusPreconditionRequired)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": false,
					"error": map[string]string{
						"code": "STEP_UP_REQUIRED",
						"message": "Complete verification to continue.",
					},
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// WriteUniformLookupResponse prevents exists/not-exists response enumeration.
// Callers perform the real lookup first, then use this identical response.
func WriteUniformLookupResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"success":true,"message":"If the resource is eligible, follow-up will be sent."}`))
}
