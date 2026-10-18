package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/stellar"
)

type stubUpgradeStore struct {
	upgrades []stellar.PendingUpgrade
}

func (s *stubUpgradeStore) ListUpgrades(_ context.Context, status string) ([]stellar.PendingUpgrade, error) {
	if status == "all" {
		return s.upgrades, nil
	}
	var out []stellar.PendingUpgrade
	for _, u := range s.upgrades {
		if u.Status == status {
			out = append(out, u)
		}
	}
	return out, nil
}

func TestProtocolUpgradeHandler_ListPendingUpgrades(t *testing.T) {
	fixedNow := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	eta := fixedNow.Add(48 * time.Hour).Unix()
	wasmHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	store := &stubUpgradeStore{
		upgrades: []stellar.PendingUpgrade{
			{
				ContractAddress: "CCWAMN2476B3R236R3T7G7M3MMK23R2J4S3I6W3M2A4J6S3I6W3M2A4J",
				WasmHash:        wasmHash,
				ETA:             eta,
				Proposer:        "GADMIN56CHARSADDRESS000000000000000000000000000000000000",
				Status:          "pending",
				ProposedLedger:  120400,
				TxHash:          "abc123txhash",
			},
			{
				ContractAddress: "CCWAMN2476B3R236R3T7G7M3MMK23R2J4S3I6W3M2A4J6S3I6W3M2A4J",
				WasmHash:        "0000000000000000000000000000000000000000000000000000000000000000",
				ETA:             fixedNow.Add(-24 * time.Hour).Unix(),
				Status:          "cancelled",
			},
		},
	}

	h := NewProtocolUpgradeHandler(store)
	h.now = func() time.Time { return fixedNow }

	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/protocol/upgrades", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Upgrades []ProtocolUpgradeItem `json:"upgrades"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Success {
		t.Fatal("expected success = true")
	}
	if len(body.Data.Upgrades) != 1 {
		t.Fatalf("upgrades len = %d, want 1", len(body.Data.Upgrades))
	}
	got := body.Data.Upgrades[0]
	if got.WasmHash != wasmHash {
		t.Fatalf("wasm_hash = %q, want %q", got.WasmHash, wasmHash)
	}
	if got.RemainingSeconds != int64((48 * time.Hour).Seconds()) {
		t.Fatalf("remaining_seconds = %d, want %d", got.RemainingSeconds, int64((48*time.Hour).Seconds()))
	}
	if got.ReleaseURL == "" {
		t.Fatal("expected non-empty release_url")
	}
}

func TestProtocolUpgradeHandler_RejectsInvalidStatusFilter(t *testing.T) {
	h := NewProtocolUpgradeHandler(&stubUpgradeStore{})
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/protocol/upgrades?status=bogus", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
