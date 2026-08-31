package signing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Result is what the signer returns for an approved intent.
//
// It contains the signed transaction envelope and nothing else. In particular
// it never contains key material, and there is no field — and no code path —
// through which a caller can ask the signer for the key.
type Result struct {
	// SignedXDR is the base64 transaction envelope, signed and ready to submit.
	SignedXDR string
	// KeyID identifies the key that produced the signature, for audit
	// correlation. It is a public identifier derived from the key, never the
	// key itself.
	KeyID string
	// IntentHash is the commitment recorded in the audit log.
	IntentHash string
}

// Backend builds and signs a transaction for a validated intent.
//
// It is the only component holding key material. It is an interface so the
// policy, audit, and kill-switch layers can be tested without a real key, and
// so an HSM- or KMS-backed implementation could be substituted later without
// touching the enforcement logic above it.
//
// Implementations MUST construct the transaction from the intent themselves.
// An implementation that signs caller-supplied bytes would reintroduce the
// signing-oracle weakness this package exists to prevent.
type Backend interface {
	// KeyID returns a public identifier for the signing key.
	KeyID() string
	// BuildAndSign constructs the transaction described by the intent and
	// signs it. It returns the signed envelope.
	BuildAndSign(ctx context.Context, i *Intent) (string, error)
}

// Service enforces the signing boundary.
//
// The order of checks is deliberate and is the security-relevant part of this
// type:
//
//  1. Kill switch — an incident halt outranks everything. If signing is
//     disabled, nothing else is even evaluated.
//  2. Structural validation — is this a well-formed request for a signable
//     operation with matching arguments?
//  3. Policy — does this deployment permit that contract, operation, amount,
//     and is the intent still fresh?
//  4. Replay — has this exact intent already been signed?
//  5. Sign.
//
// Authorization of the *caller* happens before any of this, at the transport
// layer, because an unauthenticated caller should not be able to probe policy
// behaviour at all.
//
// Every terminal outcome — including every rejection — produces an audit event.
// Rejections are the more valuable signal: a burst of them is what a compromise
// looks like from the signer side.
type Service struct {
	backend     Backend
	policy      *Policy
	killSwitch  *KillSwitch
	replayGuard *ReplayGuard
	sink        Sink
	counters    *Counters
	logger      *slog.Logger
	now         func() time.Time
}

// ServiceOptions configures a Service.
type ServiceOptions struct {
	Backend     Backend
	Policy      *Policy
	KillSwitch  *KillSwitch
	ReplayGuard *ReplayGuard
	Sink        Sink
	Counters    *Counters
	Logger      *slog.Logger
	// Now overrides the clock, for deterministic tests. Production leaves it nil.
	Now func() time.Time
}

// NewService builds the enforcement pipeline.
//
// Backend, Policy, and KillSwitch are required: a signer with no policy would
// sign anything, and a signer with no kill switch could not be halted. Both
// conditions are startup errors rather than defaults, because silently
// degrading either one produces a component that looks secure and is not.
func NewService(opts ServiceOptions) (*Service, error) {
	if opts.Backend == nil {
		return nil, errors.New("signing service requires a backend")
	}
	if opts.Policy == nil {
		return nil, errors.New("signing service requires a policy")
	}
	if opts.KillSwitch == nil {
		return nil, errors.New("signing service requires a kill switch")
	}
	sink := opts.Sink
	if sink == nil {
		sink = NewSlogSink(opts.Logger)
	}
	counters := opts.Counters
	if counters == nil {
		counters = NewCounters()
	}
	guard := opts.ReplayGuard
	if guard == nil {
		guard = NewReplayGuard(opts.Policy.MaxIntentAge + opts.Policy.ClockSkew)
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		backend:     opts.Backend,
		policy:      opts.Policy,
		killSwitch:  opts.KillSwitch,
		replayGuard: guard,
		sink:        sink,
		counters:    counters,
		logger:      logger,
		now:         nowFn,
	}, nil
}

// Sign evaluates the intent and, if permitted, signs it.
//
// caller is the authenticated identity established by the transport layer. It
// is recorded in the audit event; it is not re-verified here, because a Service
// reached without transport authentication is a deployment error that policy
// cannot compensate for.
func (s *Service) Sign(ctx context.Context, caller string, i *Intent) (*Result, error) {
	start := s.now()
	s.counters.ObserveRequest(i.Operation)

	intentHash := HashIntent(i)

	// 1. Kill switch. Checked first and unconditionally.
	if state, err := s.killSwitch.Check(); err != nil {
		s.counters.ObserveDisabled()
		s.emit(ctx, Event{
			IntentID:        i.ID,
			RequestID:       i.RequestID,
			Caller:          caller,
			Operation:       i.Operation,
			ContractAddress: i.ContractAddress,
			KeyID:           s.backend.KeyID(),
			IntentHash:      intentHash,
			Outcome:         OutcomeDisabled,
			Rejection:       RejectKillSwitchActive,
			LatencyMS:       s.elapsedMS(start),
			OccurredAt:      s.now(),
		})
		s.logger.Warn("signing request refused by kill switch",
			"intent_id", i.ID, "state", string(state))
		return nil, err
	}

	// 2. Structural validation.
	if err := i.Validate(); err != nil {
		return nil, s.rejectWith(ctx, caller, i, intentHash, start, err)
	}

	// 3. Deployment policy.
	if err := s.policy.Evaluate(i, s.now()); err != nil {
		return nil, s.rejectWith(ctx, caller, i, intentHash, start, err)
	}

	// 4. Replay. Checked after policy so that an expired-and-replayed intent
	// is reported as expired, which is the more actionable diagnosis.
	if err := s.replayGuard.Observe(i.ID, s.now()); err != nil {
		return nil, s.rejectWith(ctx, caller, i, intentHash, start, err)
	}

	// 5. Sign.
	signedXDR, err := s.backend.BuildAndSign(ctx, i)
	if err != nil {
		s.counters.ObserveError()
		s.emit(ctx, Event{
			IntentID:        i.ID,
			RequestID:       i.RequestID,
			Caller:          caller,
			Operation:       i.Operation,
			ContractAddress: i.ContractAddress,
			KeyID:           s.backend.KeyID(),
			IntentHash:      intentHash,
			Outcome:         OutcomeError,
			LatencyMS:       s.elapsedMS(start),
			OccurredAt:      s.now(),
		})
		// The backend error is wrapped without inspection. Backend errors can
		// carry transaction-building detail; they never carry key material,
		// and this path does not add any.
		return nil, fmt.Errorf("sign intent: %w", err)
	}

	latency := s.now().Sub(start)
	s.counters.ObserveSigned(i.Operation, latency)
	s.emit(ctx, Event{
		IntentID:        i.ID,
		RequestID:       i.RequestID,
		Caller:          caller,
		Operation:       i.Operation,
		ContractAddress: i.ContractAddress,
		KeyID:           s.backend.KeyID(),
		IntentHash:      intentHash,
		Outcome:         OutcomeSigned,
		LatencyMS:       latency.Milliseconds(),
		OccurredAt:      s.now(),
	})

	return &Result{
		SignedXDR:  signedXDR,
		KeyID:      s.backend.KeyID(),
		IntentHash: intentHash,
	}, nil
}

// rejectWith records a policy rejection and returns the error unchanged, so
// callers can still match on ErrPolicyRejected and read the category.
func (s *Service) rejectWith(ctx context.Context, caller string, i *Intent, intentHash string, start time.Time, err error) error {
	cat := RejectMalformed
	var pe *PolicyError
	if errors.As(err, &pe) {
		cat = pe.Category
	}
	latency := s.now().Sub(start)
	s.counters.ObserveRejected(cat, latency)
	s.emit(ctx, Event{
		IntentID:        i.ID,
		RequestID:       i.RequestID,
		Caller:          caller,
		Operation:       i.Operation,
		ContractAddress: i.ContractAddress,
		KeyID:           s.backend.KeyID(),
		IntentHash:      intentHash,
		Outcome:         OutcomeRejected,
		Rejection:       cat,
		LatencyMS:       latency.Milliseconds(),
		OccurredAt:      s.now(),
	})
	return err
}

// emit records an audit event. A sink failure is logged but does not change the
// signing decision: refusing to sign because the audit database is briefly
// unreachable would convert an observability outage into an availability
// outage. The security trade-off is explicit — the event is still emitted to
// the structured log by the fallback sink, so the record is not simply lost.
func (s *Service) emit(ctx context.Context, ev Event) {
	if err := s.sink.Record(ctx, ev); err != nil {
		s.logger.Error("failed to record signing audit event",
			"intent_id", ev.IntentID, "outcome", string(ev.Outcome), "error", err)
	}
}

func (s *Service) elapsedMS(start time.Time) int64 {
	return s.now().Sub(start).Milliseconds()
}

// Counters exposes the counter set for metrics reporting.
func (s *Service) Counters() *Counters {
	return s.counters
}

// KillSwitchStatus reports the current switch position for health reporting.
func (s *Service) KillSwitchStatus() (State, string, time.Time) {
	return s.killSwitch.Status()
}

// KeyID returns the public identifier of the signing key in use.
func (s *Service) KeyID() string {
	return s.backend.KeyID()
}
