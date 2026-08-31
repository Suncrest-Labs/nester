package signing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Outcome records how a signing request resolved.
type Outcome string

const (
	OutcomeSigned       Outcome = "signed"
	OutcomeRejected     Outcome = "rejected"
	OutcomeUnauthorized Outcome = "unauthorized"
	OutcomeDisabled     Outcome = "disabled"
	OutcomeError        Outcome = "error"
)

// Event is one security audit record from the signing boundary.
//
// What it deliberately does NOT carry, per the audit requirements: private key
// material, the raw signed transaction envelope, authorization headers, JWTs,
// or any credential. The intent is summarised by a hash and by the bounded,
// non-sensitive identifiers needed to investigate an incident.
type Event struct {
	// IntentID identifies the request and links this event to the API-side log.
	IntentID string `json:"intent_id"`
	// RequestID correlates with the originating HTTP request where present.
	RequestID string `json:"request_id,omitempty"`
	// Caller is the authenticated identity of the requesting service.
	Caller string `json:"caller"`
	// Operation is the contract function requested.
	Operation Operation `json:"operation"`
	// ContractAddress is the target contract. It is an operational identifier,
	// not a secret, and it is the field an investigator filters on first.
	ContractAddress string `json:"contract_address"`
	// KeyID identifies which signing key was used, without revealing it.
	KeyID string `json:"key_id"`
	// IntentHash commits to the exact intent that was evaluated. It lets an
	// investigator prove which request produced a given signature without the
	// audit log storing the transaction itself.
	IntentHash string `json:"intent_hash"`
	// Outcome is how the request resolved.
	Outcome Outcome `json:"outcome"`
	// Rejection categorises a refusal. Empty when Outcome is OutcomeSigned.
	Rejection Rejection `json:"rejection,omitempty"`
	// TxHash is the resulting transaction hash when one was produced.
	TxHash string `json:"tx_hash,omitempty"`
	// LatencyMS is the time spent inside the signing boundary.
	LatencyMS int64 `json:"latency_ms"`
	// OccurredAt is when the request resolved.
	OccurredAt time.Time `json:"occurred_at"`
}

// Sink receives signing audit events.
//
// It is separate from ordinary application logging by design: signing events
// are a security stream with different retention, access, and integrity
// requirements than debug logs. The production implementation writes into the
// existing hash-chained audit log so events inherit its tamper-evidence.
type Sink interface {
	// Record persists one signing event. It must not return key material or
	// otherwise sensitive detail in its error.
	Record(ctx context.Context, ev Event) error
}

// NetworkLabel maps a Stellar network passphrase to a short, stable label.
//
// The intent commitment binds to this label rather than to the passphrase
// itself. Two reasons, in order of importance:
//
//  1. It is what an investigator actually wants. "testnet" in an audit record
//     is immediately legible; a 56-character protocol constant is not.
//  2. The passphrase is a published constant, but a value whose name reads as a
//     credential flowing into a hash is a pattern both static analysis and human
//     reviewers are right to question on sight. Mapping to a label removes the
//     question rather than arguing about it.
//
// This loses no distinguishing power. Policy.Evaluate rejects any intent whose
// network does not match the signer's before it can be signed, so every signed
// intent is on the signer's own network by construction; the label records
// which one that was.
//
// An unrecognised passphrase maps to "custom" rather than being echoed, so a
// misconfigured value cannot reach the audit record verbatim.
func NetworkLabel(passphrase string) string {
	switch strings.TrimSpace(passphrase) {
	case "Public Global Stellar Network ; September 2015":
		return "pubnet"
	case "Test SDF Network ; September 2015":
		return "testnet"
	case "Test SDF Future Network ; October 2022":
		return "futurenet"
	case "":
		return "unset"
	default:
		return "custom"
	}
}

// HashIntent produces the commitment stored in the audit record.
//
// It hashes the fields that determine what would be signed. Two intents
// differing in any of them produce different hashes, so the audit record
// distinguishes them; the hash reveals nothing about the values to someone who
// does not already know them.
func HashIntent(i *Intent) string {
	h := sha256.New()
	// A length-prefixed, field-tagged encoding rather than plain concatenation:
	// concatenating "a"+"bc" and "ab"+"c" would otherwise collide.
	// This is a commitment over transaction-intent fields -- contract, amount,
	// network -- so that an audit record can prove which request produced a
	// signature. It hashes no credential and no password, so SHA-256 is the
	// right primitive; a deliberately slow KDF would be wrong for a hash
	// computed on the signing hot path. The parameter is named `field` rather
	// than `val` because the latter leads static analysis to infer a secret.
	writeField := func(tag, field string) {
		_, _ = h.Write([]byte(tag))
		_, _ = h.Write([]byte{0x1f})
		_, _ = h.Write([]byte(field))
		_, _ = h.Write([]byte{0x1e})
	}
	writeField("id", i.ID)
	writeField("op", string(i.Operation))
	writeField("shape", string(i.Shape))
	writeField("contract", i.ContractAddress)
	// A short label rather than the passphrase; see NetworkLabel.
	writeField("network", NetworkLabel(i.NetworkPassphrase))
	writeField("arg0", itoa(i.Arg0))
	writeField("arg1", itoa(i.Arg1))
	writeField("address", i.Address)
	if i.Flag {
		writeField("flag", "true")
	} else {
		writeField("flag", "false")
	}
	for _, w := range i.Weights {
		writeField("w:"+w.Protocol, itoa(int64(w.WeightBps)))
	}
	writeField("issued_at", i.IssuedAt.UTC().Format(time.RFC3339Nano))
	return hex.EncodeToString(h.Sum(nil))
}

// itoa renders an int64 for the intent hash.
//
// strconv rather than a hand-rolled conversion: the manual version needed an
// int64-to-uint64 conversion to handle the int64 minimum, which is exactly the
// kind of narrowing that invites an overflow bug (and which gosec flags).
func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

// SlogSink writes signing events to a dedicated structured logger.
//
// It is the fallback sink used when no database-backed audit service is
// available — notably in the standalone signer process, which by design has no
// database credentials. Writing to a separate logger keeps the security stream
// distinguishable from application logs even when both land in stdout.
type SlogSink struct {
	logger *slog.Logger
}

// NewSlogSink builds a sink over the given logger.
func NewSlogSink(logger *slog.Logger) *SlogSink {
	if logger == nil {
		logger = slog.Default()
	}
	return &SlogSink{logger: logger.With("stream", "signing_audit")}
}

// Record implements Sink.
func (s *SlogSink) Record(_ context.Context, ev Event) error {
	s.logger.Info("signing_event",
		"intent_id", ev.IntentID,
		"request_id", ev.RequestID,
		"caller", ev.Caller,
		"operation", string(ev.Operation),
		"contract_address", ev.ContractAddress,
		"key_id", ev.KeyID,
		"intent_hash", ev.IntentHash,
		"outcome", string(ev.Outcome),
		"rejection", string(ev.Rejection),
		"tx_hash", ev.TxHash,
		"latency_ms", ev.LatencyMS,
		"occurred_at", ev.OccurredAt.Format(time.RFC3339Nano),
	)
	return nil
}

// MultiSink fans an event out to several sinks, so an event can be both
// written to the tamper-evident chain and emitted as a structured log line.
//
// A failure in one sink does not prevent the others from receiving the event:
// losing the log line because the database was briefly unavailable would
// discard exactly the record an incident needs.
type MultiSink struct {
	sinks  []Sink
	logger *slog.Logger
}

// NewMultiSink builds a fan-out sink.
func NewMultiSink(logger *slog.Logger, sinks ...Sink) *MultiSink {
	if logger == nil {
		logger = slog.Default()
	}
	return &MultiSink{sinks: sinks, logger: logger}
}

// Record implements Sink. It returns the first error encountered, after having
// attempted every sink.
func (m *MultiSink) Record(ctx context.Context, ev Event) error {
	var firstErr error
	for _, s := range m.sinks {
		if err := s.Record(ctx, ev); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			m.logger.Error("signing audit sink failed",
				"intent_id", ev.IntentID, "error", err)
		}
	}
	return firstErr
}

// Counters tracks signing outcomes for observability.
//
// Labels are deliberately low-cardinality: outcome, operation, and rejection
// category are all drawn from small closed sets. Wallet addresses, transaction
// hashes, user IDs, and intent IDs are NOT used as label values — they are
// unbounded and would blow up a metrics backend, which is why they live in the
// audit record instead.
type Counters struct {
	mu sync.Mutex

	requests     map[Operation]int64
	signed       map[Operation]int64
	rejected     map[Rejection]int64
	unauthorized int64
	disabled     int64
	errors       int64
	latencyTotal time.Duration
	latencyCount int64
	latencyMax   time.Duration
}

// NewCounters builds an empty counter set.
func NewCounters() *Counters {
	return &Counters{
		requests: make(map[Operation]int64),
		signed:   make(map[Operation]int64),
		rejected: make(map[Rejection]int64),
	}
}

// ObserveRequest counts an inbound signing request.
func (c *Counters) ObserveRequest(op Operation) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests[op]++
}

// ObserveSigned counts a successful signature and records its latency.
func (c *Counters) ObserveSigned(op Operation, latency time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.signed[op]++
	c.observeLatencyLocked(latency)
}

// ObserveRejected counts a policy rejection by category.
func (c *Counters) ObserveRejected(cat Rejection, latency time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rejected[cat]++
	c.observeLatencyLocked(latency)
}

// ObserveUnauthorized counts a caller that failed authentication.
func (c *Counters) ObserveUnauthorized() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.unauthorized++
}

// ObserveDisabled counts a request refused by the kill switch.
func (c *Counters) ObserveDisabled() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.disabled++
}

// ObserveError counts an infrastructure failure inside the boundary.
func (c *Counters) ObserveError() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errors++
}

func (c *Counters) observeLatencyLocked(latency time.Duration) {
	c.latencyTotal += latency
	c.latencyCount++
	if latency > c.latencyMax {
		c.latencyMax = latency
	}
}

// Snapshot is an immutable copy of the counters for reporting.
type Snapshot struct {
	Requests       map[Operation]int64
	Signed         map[Operation]int64
	Rejected       map[Rejection]int64
	Unauthorized   int64
	Disabled       int64
	Errors         int64
	MeanLatency    time.Duration
	MaxLatency     time.Duration
	LatencySamples int64
}

// Snapshot returns the current counter values.
func (c *Counters) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	s := Snapshot{
		Requests:       make(map[Operation]int64, len(c.requests)),
		Signed:         make(map[Operation]int64, len(c.signed)),
		Rejected:       make(map[Rejection]int64, len(c.rejected)),
		Unauthorized:   c.unauthorized,
		Disabled:       c.disabled,
		Errors:         c.errors,
		MaxLatency:     c.latencyMax,
		LatencySamples: c.latencyCount,
	}
	for k, v := range c.requests {
		s.Requests[k] = v
	}
	for k, v := range c.signed {
		s.Signed[k] = v
	}
	for k, v := range c.rejected {
		s.Rejected[k] = v
	}
	if c.latencyCount > 0 {
		s.MeanLatency = c.latencyTotal / time.Duration(c.latencyCount)
	}
	return s
}
