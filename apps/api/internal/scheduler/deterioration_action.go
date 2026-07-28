package scheduler

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	admindomain "github.com/suncrestlabs/nester/apps/api/internal/domain/admin"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/apysnapshot"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/deterioration"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/protocoltvl"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

// deteriorationTVLWindow / deteriorationAPYWindow bound how far back
// ComputeIndicators looks. TVL uses a shorter window than the existing 24h
// drop-check because outflow *velocity* (a leading indicator) should react
// faster than the fixed 24h/20% trip-point above; APY needs a slightly
// longer window so the trailing baseline (derivedAPYBaselineWindow points)
// has enough history to be meaningful.
const (
	deteriorationTVLWindow = 6 * time.Hour
	deteriorationAPYWindow = 24 * time.Hour
)

// AutomaticRebalanceTrigger submits a vault-scoped protective rebalance
// through the existing slippage-safe, auditable rebalance mechanism —
// satisfied by *service.AdminService. Severe deterioration bounds its
// automatic action to this exact path rather than moving funds any other
// way (#857: "bounded by the slippage-safe rebalance mechanism").
type AutomaticRebalanceTrigger interface {
	TriggerRebalance(ctx context.Context, vaultID uuid.UUID, req admindomain.RebalanceRequest) (admindomain.RebalanceResponse, error)
}

// APYSnapshotLister is the narrow apysnapshot.Repository surface needed for
// indicator computation.
type APYSnapshotLister interface {
	ListByProtocol(ctx context.Context, slug string, since time.Time) ([]apysnapshot.APYSnapshot, error)
}

// DeteriorationEngine wires the pure Score/ComputeIndicators functions into
// ProtocolHealthChecker's tick: fetches the indicator history, scores it,
// records the assessment for calibration review, and dispatches graduated
// action for anything above LevelNone. Optional — ProtocolHealthChecker
// runs its existing 24h/20%-drop alert regardless of whether this is wired.
type DeteriorationEngine struct {
	tvl        protocoltvl.Repository
	apy        APYSnapshotLister
	repo       deterioration.Repository
	rebalancer AutomaticRebalanceTrigger
	notify     *notifications.Dispatcher
	logger     *slog.Logger
}

func NewDeteriorationEngine(
	tvl protocoltvl.Repository,
	apy APYSnapshotLister,
	repo deterioration.Repository,
	rebalancer AutomaticRebalanceTrigger,
	dispatcher *notifications.Dispatcher,
	logger *slog.Logger,
) *DeteriorationEngine {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &DeteriorationEngine{tvl: tvl, apy: apy, repo: repo, rebalancer: rebalancer, notify: dispatcher, logger: logger}
}

// Assess computes and records one protocol's deterioration assessment.
// Exported so it can be tested and invoked independently of the graduated
// action dispatch below.
func (e *DeteriorationEngine) Assess(ctx context.Context, slug string) (deterioration.Assessment, error) {
	now := time.Now()
	tvlSnaps, err := e.tvl.ListSince(ctx, slug, windowStart(now, deteriorationTVLWindow))
	if err != nil {
		return deterioration.Assessment{}, err
	}
	apySnaps, err := e.apy.ListByProtocol(ctx, slug, windowStart(now, deteriorationAPYWindow))
	if err != nil {
		return deterioration.Assessment{}, err
	}

	indicators := ComputeIndicators(tvlSnaps, apySnaps)
	assessment := Score(slug, indicators)
	assessment.AssessedAt = now

	if err := e.repo.RecordAssessment(ctx, assessment); err != nil {
		e.logger.Warn("deterioration engine: record assessment failed", "protocol", slug, "error", err)
	}
	return assessment, nil
}

// DispatchAction maps assessment.Level to proportionate action and audits
// it (#857's graduated-alert requirement). vaults is the set of active
// vaults allocated to this protocol — only needed for LevelSevere (the
// automatic rebalance moves each affected vault); mild/moderate are
// protocol-wide recommendations, not per-vault actions.
func (e *DeteriorationEngine) DispatchAction(ctx context.Context, assessment deterioration.Assessment, vaults []vault.Vault) {
	switch assessment.Level {
	case deterioration.LevelNone:
		return
	case deterioration.LevelMild:
		e.recordAction(ctx, assessment, deterioration.ActionCeilingCut, nil, nil, "")
	case deterioration.LevelModerate:
		e.recordAction(ctx, assessment, deterioration.ActionRecommendRebalance, nil, nil, "")
		e.notifyOperators(ctx, assessment)
	case deterioration.LevelSevere:
		e.notifyOperators(ctx, assessment)
		e.triggerAutomaticRebalances(ctx, assessment, vaults)
	}
}

// triggerAutomaticRebalances submits a protective rebalance for every
// active vault allocated to the deteriorating protocol. Each attempt
// (success or failure) is individually audited — #857 is explicit that
// automatic capital movement must never be silent, including when it
// fails.
func (e *DeteriorationEngine) triggerAutomaticRebalances(ctx context.Context, assessment deterioration.Assessment, vaults []vault.Vault) {
	if e.rebalancer == nil {
		e.logger.Warn("deterioration engine: severe assessment but no rebalance trigger configured", "protocol", assessment.ProtocolSlug)
		return
	}
	for _, v := range vaults {
		if !vaultHasAllocation(v, assessment.ProtocolSlug) {
			continue
		}
		vaultID := v.ID
		resp, err := e.rebalancer.TriggerRebalance(ctx, vaultID, admindomain.RebalanceRequest{
			Strategy: admindomain.RebalanceStrategyAuto,
			DryRun:   false,
		})
		if err != nil {
			e.logger.Error("deterioration engine: automatic rebalance failed", "protocol", assessment.ProtocolSlug, "vault_id", vaultID, "error", err)
			e.recordAction(ctx, assessment, deterioration.ActionAutomaticRebalance, &vaultID, nil, err.Error())
			continue
		}
		e.logger.Warn("deterioration engine: automatic protective rebalance triggered",
			"protocol", assessment.ProtocolSlug, "vault_id", vaultID, "rebalance_id", resp.RebalanceID, "probability", assessment.Probability,
		)
		rebalanceID := resp.RebalanceID
		e.recordAction(ctx, assessment, deterioration.ActionAutomaticRebalance, &vaultID, &rebalanceID, "")
		e.notifyVaultOwner(ctx, v, assessment)
	}
}

func vaultHasAllocation(v vault.Vault, protocolSlug string) bool {
	for _, a := range v.Allocations {
		if strings.ToLower(strings.TrimSpace(a.Protocol)) == protocolSlug {
			return true
		}
	}
	return false
}

func (e *DeteriorationEngine) recordAction(
	ctx context.Context,
	assessment deterioration.Assessment,
	kind deterioration.ActionKind,
	vaultID, rebalanceID *uuid.UUID,
	errMsg string,
) {
	action := &deterioration.Action{
		ProtocolSlug: assessment.ProtocolSlug,
		Level:        assessment.Level,
		Probability:  assessment.Probability,
		Kind:         kind,
		VaultID:      vaultID,
		RebalanceID:  rebalanceID,
		Explanation:  assessment.Explanation,
		Error:        errMsg,
	}
	if err := e.repo.RecordAction(ctx, action); err != nil {
		e.logger.Error("deterioration engine: failed to persist action audit record", "protocol", assessment.ProtocolSlug, "kind", kind, "error", err)
	}
}

func (e *DeteriorationEngine) notifyOperators(ctx context.Context, assessment deterioration.Assessment) {
	e.logger.Warn("protocol deterioration alert",
		"protocol", assessment.ProtocolSlug,
		"level", assessment.Level,
		"probability", assessment.Probability,
		"explanation", assessment.Explanation,
	)
}

// notifyVaultOwner tells the affected user their funds were moved and why
// (#857: "explaining to users why the platform reduced exposure to a
// source").
func (e *DeteriorationEngine) notifyVaultOwner(ctx context.Context, v vault.Vault, assessment deterioration.Assessment) {
	if e.notify == nil {
		return
	}
	title := "We moved your funds to reduce risk"
	body := "Your vault's allocation to " + assessment.ProtocolSlug + " showed signs of rising risk (" + assessment.Explanation + "), so we automatically reduced your exposure."
	_ = e.notify.Send(ctx, v.UserID, notifications.EventProtocolHealthAlert, title, body, map[string]any{
		"protocol":    assessment.ProtocolSlug,
		"vault_id":    v.ID.String(),
		"probability": assessment.Probability,
		"explanation": assessment.Explanation,
	})
}
