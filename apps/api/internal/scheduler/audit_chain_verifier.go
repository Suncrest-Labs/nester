package scheduler

import (
	"context"
	"log/slog"
	"time"
)

// ChainVerifier is a narrow interface that exposes only the verification
// capability from audit.AuditService.  Using a narrow interface keeps the
// scheduler package free from the full audit package dependency and makes
// the verifier trivially testable without satisfying every audit method.
type ChainVerifier interface {
	VerifyChain(ctx context.Context, fromSeq, toSeq int64) (bool, int64, error)
}

// AuditChainVerifierConfig controls how often the chain integrity job runs.
type AuditChainVerifierConfig struct {
	// Enabled disables the job entirely when false. The job is skipped silently.
	Enabled bool
	// Interval is how often the verifier runs. Recommended: every 15–60 minutes.
	Interval time.Duration
	// BatchSize is the number of entries verified per run. 0 means verify all entries.
	BatchSize int64
}

// ChainBreakAlerter is a callback invoked when the verifier finds a break in the chain.
// Production callers should page on-call engineers or emit a critical metric.
type ChainBreakAlerter interface {
	AlertChainBreak(ctx context.Context, brokenAtSeq int64, reason error)
}

// ChainBreakAlertFunc is a convenience adapter for ChainBreakAlerter.
type ChainBreakAlertFunc func(ctx context.Context, brokenAtSeq int64, reason error)

func (f ChainBreakAlertFunc) AlertChainBreak(ctx context.Context, brokenAtSeq int64, reason error) {
	f(ctx, brokenAtSeq, reason)
}

// AuditChainVerifier is a background job that periodically recomputes hashes
// over the audit log chain and raises a critical alert on any break.
//
// This satisfies the issue's requirement: "A scheduled verification job runs
// continuously and raises a critical alert on any break."
type AuditChainVerifier struct {
	cfg     AuditChainVerifierConfig
	svc     ChainVerifier
	alerter ChainBreakAlerter
	logger  *slog.Logger
}

// NewAuditChainVerifier constructs a new AuditChainVerifier.
func NewAuditChainVerifier(
	cfg AuditChainVerifierConfig,
	svc ChainVerifier,
	alerter ChainBreakAlerter,
	logger *slog.Logger,
) *AuditChainVerifier {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &AuditChainVerifier{
		cfg:     cfg,
		svc:     svc,
		alerter: alerter,
		logger:  logger,
	}
}

// Run drives the verification loop until the context is cancelled.
// When Enabled is false Run returns immediately.
func (v *AuditChainVerifier) Run(ctx context.Context) {
	if !v.cfg.Enabled {
		v.logger.Info("audit chain verifier: disabled, not starting")
		return
	}

	interval := v.cfg.Interval
	if interval == 0 {
		interval = 30 * time.Minute
	}

	v.logger.Info("audit chain verifier: starting", "interval", interval)
	v.runOnce(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			v.logger.Info("audit chain verifier: stopping")
			return
		case <-ticker.C:
			v.runOnce(ctx)
		}
	}
}

// RunOnce performs a single verification pass. Exposed for manual operator runs.
func (v *AuditChainVerifier) RunOnce(ctx context.Context) (bool, int64, error) {
	from := int64(1)
	to := int64(^uint64(0) >> 1) // MaxInt64 — verify the entire chain

	if v.cfg.BatchSize > 0 {
		to = from + v.cfg.BatchSize - 1
	}

	return v.svc.VerifyChain(ctx, from, to)
}

func (v *AuditChainVerifier) runOnce(ctx context.Context) {
	start := time.Now()
	ok, brokenSeq, err := v.RunOnce(ctx)
	elapsed := time.Since(start)

	if err != nil || !ok {
		v.logger.Error("audit chain verifier: CHAIN INTEGRITY BREAK DETECTED",
			"broken_at_sequence", brokenSeq,
			"error", err,
			"elapsed_ms", elapsed.Milliseconds(),
		)
		if v.alerter != nil {
			v.alerter.AlertChainBreak(ctx, brokenSeq, err)
		}
		return
	}

	v.logger.Info("audit chain verifier: chain integrity OK",
		"elapsed_ms", elapsed.Milliseconds(),
	)
}
