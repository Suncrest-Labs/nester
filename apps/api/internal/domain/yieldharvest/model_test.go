package yieldharvest

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestErrNotFound_Message(t *testing.T) {
	if ErrNotFound == nil {
		t.Fatal("ErrNotFound must not be nil")
	}
	if ErrNotFound.Error() != "yield harvest not found" {
		t.Errorf("got %q, want %q", ErrNotFound.Error(), "yield harvest not found")
	}
}

func TestYieldHarvest_AmountPrecision(t *testing.T) {
	// Reward calculations must not lose precision at small decimal values.
	raw := "0.000000000000000001"
	amount, err := decimal.NewFromString(raw)
	if err != nil {
		t.Fatalf("decimal.NewFromString: %v", err)
	}
	h := YieldHarvest{Amount: amount}
	if h.Amount.String() != raw {
		t.Errorf("precision lost: got %q, want %q", h.Amount.String(), raw)
	}
}

func TestListFilter_SinceUntilWindow(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	f := ListFilter{
		UserID: uuid.New(),
		Since:  &start,
		Until:  &end,
		Limit:  50,
	}
	if !f.Since.Equal(start) {
		t.Errorf("Since: got %v, want %v", f.Since, start)
	}
	if !f.Until.Equal(end) {
		t.Errorf("Until: got %v, want %v", f.Until, end)
	}
	if f.Limit != 50 {
		t.Errorf("Limit: got %d, want 50", f.Limit)
	}
}

func TestListFilter_NilCursorByDefault(t *testing.T) {
	f := ListFilter{UserID: uuid.New(), Limit: 20}
	if f.CursorTime != nil {
		t.Error("CursorTime should be nil when not set")
	}
	if f.CursorID != nil {
		t.Error("CursorID should be nil when not set")
	}
}
