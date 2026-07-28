package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/activity"
)

type stubActivityLister struct {
	items      []activity.Item
	nextCursor string
	prevCursor string
	err        error
	gotFilter  activity.ListFilter
}

func (s *stubActivityLister) List(_ context.Context, _ uuid.UUID, filter activity.ListFilter) ([]activity.Item, string, string, error) {
	s.gotFilter = filter
	if s.err != nil {
		return nil, "", "", s.err
	}
	return s.items, s.nextCursor, s.prevCursor, nil
}

func TestActivityHandler_List_RequiresAuth(t *testing.T) {
	h := NewActivityHandler(&stubActivityLister{})
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/activity")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestActivityHandler_List_MapsToFrontendContract(t *testing.T) {
	userID := uuid.New()
	itemID := uuid.New()
	vaultID := uuid.New()
	createdAt := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	stub := &stubActivityLister{
		items: []activity.Item{
			{
				ID:        itemID,
				Type:      activity.EventDeposit,
				Amount:    decimal.RequireFromString("123.45"),
				Currency:  "USDC",
				Status:    activity.StatusCompleted,
				CreatedAt: createdAt,
				VaultID:   vaultID,
				VaultName: "USDC Vault",
				Ref:       "txhash-1",
			},
		},
		nextCursor: "next-token",
		prevCursor: "",
	}
	h := NewActivityHandler(stub)
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(withAuthUser(mux, userID))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/activity?type=Deposit,Settlement&status=Confirmed&limit=10")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Data []struct {
			ID        string  `json:"id"`
			Timestamp string  `json:"timestamp"`
			Type      string  `json:"type"`
			VaultName string  `json:"vaultName"`
			Amount    float64 `json:"amount"`
			Asset     string  `json:"asset"`
			Status    string  `json:"status"`
			TxHash    string  `json:"txHash"`
		} `json:"data"`
		NextCursor string `json:"nextCursor"`
		PrevCursor string `json:"prevCursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	if len(body.Data) != 1 {
		t.Fatalf("got %d items, want 1", len(body.Data))
	}
	got := body.Data[0]
	if got.Type != "Deposit" {
		t.Fatalf("Type = %q, want %q", got.Type, "Deposit")
	}
	if got.Status != "Confirmed" {
		t.Fatalf("Status = %q, want %q", got.Status, "Confirmed")
	}
	if got.Amount != 123.45 {
		t.Fatalf("Amount = %v, want 123.45", got.Amount)
	}
	if got.VaultName != "USDC Vault" || got.Asset != "USDC" || got.TxHash != "txhash-1" {
		t.Fatalf("unexpected item shape: %+v", got)
	}
	if body.NextCursor != "next-token" || body.PrevCursor != "" {
		t.Fatalf("cursors = next=%q prev=%q, want next=%q prev=empty", body.NextCursor, body.PrevCursor, "next-token")
	}

	if len(stub.gotFilter.Types) != 2 || stub.gotFilter.Types[0] != activity.EventDeposit || stub.gotFilter.Types[1] != activity.EventSettlement {
		t.Fatalf("type filter not parsed correctly: %+v", stub.gotFilter.Types)
	}
	if stub.gotFilter.Status != activity.StatusCompleted {
		t.Fatalf("status filter = %q, want %q", stub.gotFilter.Status, activity.StatusCompleted)
	}
}

func TestActivityHandler_List_RejectsUnknownType(t *testing.T) {
	userID := uuid.New()
	h := NewActivityHandler(&stubActivityLister{})
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(withAuthUser(mux, userID))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/activity?type=NotAType")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
