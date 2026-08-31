package reconciliation

import (
	"time"

	"github.com/shopspring/decimal"
)

type Classifier struct {
	DustTolerance     decimal.Decimal
	WarningThreshold  decimal.Decimal
	CriticalThreshold decimal.Decimal
	StuckAfter        time.Duration
}

func NewClassifier(cfg Classifier) Classifier {
	if cfg.DustTolerance.IsZero() {
		cfg.DustTolerance = decimal.RequireFromString("0.000001")
	}
	if cfg.WarningThreshold.IsZero() {
		cfg.WarningThreshold = decimal.RequireFromString("0.01")
	}
	if cfg.CriticalThreshold.IsZero() {
		cfg.CriticalThreshold = decimal.RequireFromString("1")
	}
	if cfg.StuckAfter <= 0 {
		cfg.StuckAfter = 30 * time.Minute
	}
	return cfg
}

func (c Classifier) Classify(input FindingInput, observedAt time.Time) Finding {
	c = NewClassifier(c)
	finding := Finding{
		Level:           input.Level,
		Type:            input.Type,
		EntityType:      input.EntityType,
		EntityID:        input.EntityID,
		RecordedValue:   input.RecordedValue,
		OnChainValue:    input.OnChainValue,
		Tolerance:       c.DustTolerance,
		ObservedAt:      observedAt.UTC(),
		Details:         input.Details,
		ResolutionState: ResolutionOpen,
	}

	if input.RecordedValue != nil && input.OnChainValue != nil {
		diff := input.RecordedValue.Sub(*input.OnChainValue).Abs()
		finding.Difference = &diff
		finding.Severity = c.severityForDifference(diff)
		return finding
	}

	if input.Type == TypeStuck && input.Age >= c.StuckAfter {
		finding.Severity = SeverityWarning
		return finding
	}
	if input.Type == TypeMissing || input.Type == TypeExtra {
		finding.Severity = SeverityCritical
		return finding
	}

	finding.Severity = SeverityInformational
	return finding
}

func (c Classifier) severityForDifference(diff decimal.Decimal) Severity {
	if diff.LessThanOrEqual(c.DustTolerance) {
		return SeverityInformational
	}
	if diff.GreaterThanOrEqual(c.CriticalThreshold) {
		return SeverityCritical
	}
	if diff.GreaterThanOrEqual(c.WarningThreshold) {
		return SeverityWarning
	}
	return SeverityInformational
}
