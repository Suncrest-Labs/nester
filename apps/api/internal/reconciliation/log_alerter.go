package reconciliation

import (
	"context"
	"log/slog"
)

// LogAlerter dispatches findings to structured logs. It is the production
// Alerter (nester#1082): the pager itself rides the Prometheus divergence
// counter (ReconciliationDivergence fires on any recorded finding), so what
// the alert path owes the operator is not another delivery channel but the
// JOIN KEY — the divergence metric is deliberately unlabelled by vault or
// user (cardinality policy, docs/observability/slo.md), and the runbook sends
// whoever was paged to the logs to find which records are affected.
type LogAlerter struct {
	logger *slog.Logger
}

// NewLogAlerter builds an alerter over the given logger. A nil logger falls
// back to slog.Default, matching the Engine's own convention.
func NewLogAlerter(logger *slog.Logger) *LogAlerter {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogAlerter{logger: logger}
}

// CriticalFinding logs at Error level — a critical divergence is the most
// serious signal this system produces.
func (a *LogAlerter) CriticalFinding(ctx context.Context, finding Finding) error {
	a.logger.ErrorContext(ctx, "reconciliation divergence", a.attrs(finding)...)
	return nil
}

// WarningFinding logs at Warn level.
func (a *LogAlerter) WarningFinding(ctx context.Context, finding Finding) error {
	a.logger.WarnContext(ctx, "reconciliation divergence", a.attrs(finding)...)
	return nil
}

// attrs renders the finding fields the runbook's triage procedure needs:
// enough to locate the exact record and see both sides of the disagreement
// without a database query.
func (a *LogAlerter) attrs(finding Finding) []any {
	attrs := []any{
		"finding_id", finding.ID.String(),
		"run_id", finding.RunID.String(),
		"level", string(finding.Level),
		"type", string(finding.Type),
		"severity", string(finding.Severity),
		"entity_type", finding.EntityType,
		"entity_id", finding.EntityID,
	}
	if finding.RecordedValue != nil {
		attrs = append(attrs, "recorded_value", finding.RecordedValue.String())
	}
	if finding.OnChainValue != nil {
		attrs = append(attrs, "on_chain_value", finding.OnChainValue.String())
	}
	if finding.Difference != nil {
		attrs = append(attrs, "difference", finding.Difference.String())
	}
	for key, value := range finding.Details {
		attrs = append(attrs, "detail_"+key, value)
	}
	return attrs
}
