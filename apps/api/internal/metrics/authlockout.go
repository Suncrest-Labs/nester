package metrics

import "github.com/prometheus/client_golang/prometheus"

// Scope labels for the auth-failure collectors. The label set is deliberately
// closed: the *values* being counted (wallet addresses, client IPs) are
// unbounded and attacker-controlled, so they must never become label values —
// that would let a single attacker create a series per address and exhaust
// Prometheus' memory. Only the scope is labelled.
const (
	// AuthScopeWallet counts activity keyed by the claimed wallet address.
	AuthScopeWallet = "wallet"
	// AuthScopeIP counts activity keyed by the client IP.
	AuthScopeIP = "ip"
)

// Auth stages, so a lockout on the signature check is distinguishable from one
// on challenge issuance.
const (
	AuthStageChallenge = "challenge"
	AuthStageVerify    = "verify"
)

// authCollectors instrument the auth challenge/verify hardening (nester#1104).
//
// The pair is the useful signal. failures_total rising alone means users are
// fumbling signatures; lockouts_total rising means the backoff has actually
// engaged and something is hammering the endpoint. An alert watches the
// second, a capacity review reads the first.
type authCollectors struct {
	failures *prometheus.CounterVec
	lockouts *prometheus.CounterVec
	rejected *prometheus.CounterVec
}

func newAuthCollectors() *authCollectors {
	labels := []string{"scope", "stage"}

	return &authCollectors{
		// Every recorded auth failure, before any lockout decision.
		failures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: "auth",
			Name:      "failures_total",
			Help:      "Authentication failures recorded, by key scope and auth stage.",
		}, labels),

		// Counts the transitions into a locked state — the moment a key
		// crosses the failure threshold and a backoff is applied. This is the
		// observable the issue asks for: it moves only under sustained abuse.
		lockouts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: "auth",
			Name:      "lockouts_total",
			Help:      "Authentication lockouts triggered by repeated failures, by key scope and auth stage.",
		}, labels),

		// Requests refused because a lockout was already in force. Rated
		// against lockouts_total this shows how hard a locked-out client keeps
		// trying, which distinguishes a confused user from a running attack.
		rejected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: "auth",
			Name:      "locked_requests_total",
			Help:      "Requests rejected because an authentication lockout was in force, by key scope and auth stage.",
		}, labels),
	}
}

func (c *authCollectors) collectors() []prometheus.Collector {
	return []prometheus.Collector{c.failures, c.lockouts, c.rejected}
}

// RecordAuthFailure counts one authentication failure for a key scope.
func (m *Metrics) RecordAuthFailure(scope, stage string) {
	if m == nil {
		return
	}
	m.auth.failures.WithLabelValues(scope, stage).Inc()
}

// RecordAuthLockout counts one key entering a locked state.
func (m *Metrics) RecordAuthLockout(scope, stage string) {
	if m == nil {
		return
	}
	m.auth.lockouts.WithLabelValues(scope, stage).Inc()
}

// RecordAuthLockedRequest counts one request refused by an active lockout.
func (m *Metrics) RecordAuthLockedRequest(scope, stage string) {
	if m == nil {
		return
	}
	m.auth.rejected.WithLabelValues(scope, stage).Inc()
}
