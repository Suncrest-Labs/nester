package nudge

import (
	"strconv"
	"time"

	"github.com/shopspring/decimal"
)

type Facts struct {
	GoalName      string
	TargetAmount  decimal.Decimal
	CurrentAmount decimal.Decimal
	Currency      string
	Deadline      time.Time
	StreakWeeks   int
	APY           float64
}

// AllowedFacts creates the grounded string->string fact map an LLM copy
// generator is given as context and the numeric-grounding check validates
// its output against. Every field that can end up quoted in generated
// copy must be present here, or the grounding check has nothing to verify
// it against.
func (f Facts) AllowedFacts() map[string]string {
	m := make(map[string]string)
	if f.GoalName != "" {
		m["GoalName"] = f.GoalName
	}
	if f.TargetAmount.IsPositive() {
		m["TargetAmount"] = f.TargetAmount.String()
	}
	if f.CurrentAmount.IsPositive() {
		m["CurrentAmount"] = f.CurrentAmount.String()
	}
	if f.Currency != "" {
		m["Currency"] = f.Currency
	}
	if !f.Deadline.IsZero() {
		m["Deadline"] = f.Deadline.Format("Jan 02")
	}
	if f.StreakWeeks > 0 {
		m["StreakWeeks"] = strconv.Itoa(f.StreakWeeks)
	}
	if f.APY > 0 {
		m["APY"] = strconv.FormatFloat(f.APY, 'f', -1, 64)
	}
	return m
}
