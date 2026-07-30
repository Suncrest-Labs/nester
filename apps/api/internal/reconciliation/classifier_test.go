package reconciliation

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestClassifierClassifiesDustAsInformational(t *testing.T) {
	classifier := NewClassifier(Classifier{
		DustTolerance:     decimal.RequireFromString("0.01"),
		WarningThreshold:  decimal.RequireFromString("1"),
		CriticalThreshold: decimal.RequireFromString("100"),
	})
	recorded := decimal.RequireFromString("100.001")
	onChain := decimal.RequireFromString("100.000")

	finding := classifier.Classify(FindingInput{
		Level:         LevelBalance,
		Type:          TypeMismatch,
		EntityType:    "vault",
		EntityID:      "vault-1",
		RecordedValue: &recorded,
		OnChainValue:  &onChain,
	}, time.Now())

	if finding.Severity != SeverityInformational {
		t.Fatalf("severity = %q, want informational", finding.Severity)
	}
}

func TestClassifierClassifiesMaterialMismatchAsCritical(t *testing.T) {
	classifier := NewClassifier(Classifier{
		DustTolerance:     decimal.RequireFromString("0.01"),
		WarningThreshold:  decimal.RequireFromString("1"),
		CriticalThreshold: decimal.RequireFromString("100"),
	})
	recorded := decimal.RequireFromString("250")
	onChain := decimal.RequireFromString("100")

	finding := classifier.Classify(FindingInput{
		Level:         LevelBalance,
		Type:          TypeMismatch,
		EntityType:    "vault",
		EntityID:      "vault-1",
		RecordedValue: &recorded,
		OnChainValue:  &onChain,
	}, time.Now())

	if finding.Severity != SeverityCritical {
		t.Fatalf("severity = %q, want critical", finding.Severity)
	}
}

func TestClassifierClassifiesStuckTransaction(t *testing.T) {
	classifier := NewClassifier(Classifier{StuckAfter: 30 * time.Minute})

	finding := classifier.Classify(FindingInput{
		Level:      LevelTransaction,
		Type:       TypeStuck,
		EntityType: "transaction",
		EntityID:   "hash",
		Age:        time.Hour,
	}, time.Now())

	if finding.Severity != SeverityWarning {
		t.Fatalf("severity = %q, want warning", finding.Severity)
	}
}
