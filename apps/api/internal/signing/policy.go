package signing

import (
	"strings"
	"sync"
	"time"
)

// Policy is the deployment-specific authorization layer applied to an intent
// after its structure has been validated.
//
// Structural validation (Intent.Validate) answers "is this a well-formed
// request for a signable operation?". Policy answers "is this deployment
// willing to sign it?" — which contracts are in scope, how large an amount may
// be, how long an intent stays valid. The split matters because the structural
// rules are invariants of the protocol while the policy is an operational
// choice that differs between testnet and production.
type Policy struct {
	// NetworkPassphrase pins the network this signer serves. An intent
	// declaring a different network is rejected. This is what prevents a
	// misconfigured or compromised caller from obtaining a mainnet signature
	// from a testnet signer, or vice versa.
	NetworkPassphrase string

	// AllowedContracts restricts which contract addresses may be invoked.
	// An empty set means no contract is allowed — the signer fails closed
	// rather than defaulting to permitting everything.
	AllowedContracts map[string]struct{}

	// AllowedOperations restricts which operations this deployment permits,
	// as a subset of the operations the signer knows how to build. An empty
	// set means none are allowed, again failing closed.
	AllowedOperations map[Operation]struct{}

	// MaxAmountStroops bounds the primary amount on deposit and withdraw
	// intents. It is the single most valuable limit in the policy: it caps the
	// per-transaction damage a compromised API can cause even when it requests
	// an otherwise entirely legitimate operation.
	MaxAmountStroops int64

	// MaxIntentAge bounds how long an intent remains signable after issue.
	// A captured intent stops being useful once it expires, which limits the
	// value of replaying traffic recorded from the boundary.
	MaxIntentAge time.Duration

	// ClockSkew tolerates a small amount of clock drift between the API and
	// signer processes when checking IssuedAt against the current time.
	// Without it, an intent issued microseconds in the future on a slightly
	// fast API clock would be rejected as malformed.
	ClockSkew time.Duration
}

// DefaultMaxIntentAge is the intent validity window when none is configured.
// Two minutes comfortably exceeds the observed signing path latency (simulate,
// build, sign, submit) while keeping a captured intent short-lived.
const DefaultMaxIntentAge = 2 * time.Minute

// DefaultClockSkew is the tolerated clock drift between API and signer.
const DefaultClockSkew = 30 * time.Second

// NewPolicy builds a policy from the allowed contract and operation lists.
//
// Both lists are required to be non-empty. A signer configured with no allowed
// contracts, or no allowed operations, can sign nothing — that is the intended
// fail-closed behaviour, and callers are expected to treat an empty
// configuration as a deployment error rather than a permissive default.
func NewPolicy(networkPassphrase string, contracts []string, operations []Operation, maxAmountStroops int64, maxIntentAge, clockSkew time.Duration) *Policy {
	allowedContracts := make(map[string]struct{}, len(contracts))
	for _, c := range contracts {
		if c = strings.TrimSpace(c); c != "" {
			allowedContracts[c] = struct{}{}
		}
	}
	allowedOps := make(map[Operation]struct{}, len(operations))
	for _, op := range operations {
		if _, known := ShapeFor(op); known {
			allowedOps[op] = struct{}{}
		}
	}
	if maxIntentAge <= 0 {
		maxIntentAge = DefaultMaxIntentAge
	}
	if clockSkew <= 0 {
		clockSkew = DefaultClockSkew
	}
	return &Policy{
		NetworkPassphrase: strings.TrimSpace(networkPassphrase),
		AllowedContracts:  allowedContracts,
		AllowedOperations: allowedOps,
		MaxAmountStroops:  maxAmountStroops,
		MaxIntentAge:      maxIntentAge,
		ClockSkew:         clockSkew,
	}
}

// Evaluate applies the policy to a structurally valid intent at time now.
//
// Callers must run Intent.Validate first; Evaluate assumes the shape and
// argument checks have already passed and concerns itself only with deployment
// authorization. SignerService runs both in order, so callers going through the
// signer get both layers regardless.
func (p *Policy) Evaluate(i *Intent, now time.Time) error {
	// Network first: signing for the wrong network is the most consequential
	// mismatch, and checking it before anything else means a testnet/mainnet
	// confusion is reported as exactly that rather than as some downstream
	// symptom.
	if p.NetworkPassphrase == "" {
		return reject(RejectNetworkMismatch, "signer has no configured network")
	}
	if i.NetworkPassphrase != p.NetworkPassphrase {
		// The configured and requested passphrases are not echoed: the
		// passphrase is not secret, but including attacker-supplied text in an
		// audit record invites log injection. The category carries the meaning.
		return reject(RejectNetworkMismatch, "intent network does not match the signer network")
	}

	if len(p.AllowedOperations) == 0 {
		return reject(RejectUnknownOperation, "signer permits no operations")
	}
	if _, ok := p.AllowedOperations[i.Operation]; !ok {
		return reject(RejectUnknownOperation, "operation %q is not permitted by this signer", i.Operation)
	}

	if len(p.AllowedContracts) == 0 {
		return reject(RejectContractNotAllowed, "signer permits no contracts")
	}
	if _, ok := p.AllowedContracts[strings.TrimSpace(i.ContractAddress)]; !ok {
		// The address is not echoed for the same log-injection reason; the
		// intent ID in the audit record identifies which request this was.
		return reject(RejectContractNotAllowed, "contract address is not in the signer allowlist")
	}

	// Amount bounds apply to the value-moving operations. pause, rebalance and
	// the rest carry no amount, so there is nothing to bound.
	if i.Shape == ShapeI128Pair && p.MaxAmountStroops > 0 && i.Arg0 > p.MaxAmountStroops {
		return reject(RejectAmountOutOfPolicy,
			"amount %d exceeds the configured per-transaction limit of %d stroops",
			i.Arg0, p.MaxAmountStroops)
	}

	// Expiry. An intent issued too far in the past has either sat in a queue
	// too long or was captured and replayed; either way it is refused.
	age := now.Sub(i.IssuedAt)
	if age > p.MaxIntentAge {
		return reject(RejectIntentExpired,
			"intent is %s old, limit is %s", age.Truncate(time.Second), p.MaxIntentAge)
	}
	// An intent from the future beyond tolerated skew indicates a clock problem
	// or a forged timestamp intended to extend the validity window.
	if age < -p.ClockSkew {
		return reject(RejectIntentExpired,
			"intent issue time is %s in the future, beyond the %s skew tolerance",
			(-age).Truncate(time.Second), p.ClockSkew)
	}

	return nil
}

// ReplayGuard rejects an intent ID that has already been signed.
//
// Scope and honesty about it: this is an in-memory guard local to one signer
// process. It stops the same intent being signed twice by that process within
// the retention window, which is the replay vector reachable from a compromised
// API re-sending a captured request. It does not survive a restart, and it does
// not coordinate across multiple signer replicas.
//
// That is sufficient for the current single-signer deployment, and the limit is
// documented rather than papered over. A multi-replica deployment needs a
// shared store; the interface here is deliberately small enough to swap.
type ReplayGuard struct {
	mu        sync.Mutex
	seen      map[string]time.Time
	retention time.Duration
	// lastSweep bounds how often the expiry sweep runs, so a burst of intents
	// does not walk the whole map on every call.
	lastSweep time.Time
}

// NewReplayGuard builds a guard retaining IDs for at least the given duration.
// Retention should be at least the policy MaxIntentAge plus the clock skew
// tolerance: an intent that can still pass the expiry check must still be
// recognised as replayed.
func NewReplayGuard(retention time.Duration) *ReplayGuard {
	if retention <= 0 {
		retention = DefaultMaxIntentAge + DefaultClockSkew
	}
	return &ReplayGuard{
		seen:      make(map[string]time.Time),
		retention: retention,
	}
}

// Observe records the intent ID and reports whether it had already been seen.
// It returns a PolicyError with RejectIntentReplayed when the ID is a repeat,
// so callers can treat it identically to any other policy rejection.
func (g *ReplayGuard) Observe(id string, now time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.sweepLocked(now)

	if _, dup := g.seen[id]; dup {
		return reject(RejectIntentReplayed, "intent id has already been signed")
	}
	g.seen[id] = now
	return nil
}

// sweepInterval bounds how often expired entries are purged.
const sweepInterval = 30 * time.Second

func (g *ReplayGuard) sweepLocked(now time.Time) {
	if now.Sub(g.lastSweep) < sweepInterval {
		return
	}
	g.lastSweep = now
	cutoff := now.Add(-g.retention)
	for id, seenAt := range g.seen {
		if seenAt.Before(cutoff) {
			delete(g.seen, id)
		}
	}
}

// Size reports how many IDs are currently retained. Exposed for tests and for
// an operational gauge; it carries no sensitive data.
func (g *ReplayGuard) Size() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.seen)
}
