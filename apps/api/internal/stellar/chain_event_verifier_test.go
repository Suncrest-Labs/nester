package stellar

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stellar/go/strkey"
	"github.com/stellar/go/xdr"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

func TestRPCChainEventVerifier_RequiresHash(t *testing.T) {
	v := NewRPCChainEventVerifier("http://rpc")
	_, err := v.VerifyVaultEvent(context.Background(), "", "CABC", "withdraw")
	if !errors.Is(err, vault.ErrTxHashRequired) {
		t.Fatalf("err = %v, want ErrTxHashRequired", err)
	}
}

func TestRPCChainEventVerifier_RejectsFailedTx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]any{"status": "FAILED"},
		})
	}))
	t.Cleanup(srv.Close)

	v := NewRPCChainEventVerifier(srv.URL)
	_, err := v.VerifyVaultEvent(context.Background(), "abc", "CABC", "withdraw")
	if !errors.Is(err, vault.ErrUnverifiedChainTx) {
		t.Fatalf("err = %v, want ErrUnverifiedChainTx", err)
	}
}

func TestRPCChainEventVerifier_ReadsWithdrawAmountFromMeta(t *testing.T) {
	contract := "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAD2KM"
	raw, err := strkey.Decode(strkey.VersionByteContract, contract)
	if err != nil {
		t.Fatalf("decode contract: %v", err)
	}
	var contractID xdr.ContractId
	copy(contractID[:], raw)

	amountSym := xdr.ScSymbol("amount")
	withdrawSym := xdr.ScSymbol("WITHDRAW")
	stroops := xdr.Int128Parts{Hi: 0, Lo: xdr.Uint64(250_000_000)} // 25.0000000 USDC
	amountMap := xdr.ScMap{
		{
			Key: xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &amountSym},
			Val: xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &stroops},
		},
	}
	amountMapPtr := &amountMap
	body := xdr.ContractEventBody{
		V: 0,
		V0: &xdr.ContractEventV0{
			Topics: []xdr.ScVal{{Type: xdr.ScValTypeScvSymbol, Sym: &withdrawSym}},
			Data: xdr.ScVal{
				Type: xdr.ScValTypeScvMap,
				Map:  &amountMapPtr,
			},
		},
	}
	ce := xdr.ContractEvent{
		ContractId: &contractID,
		Type:       xdr.ContractEventTypeContract,
		Body:       body,
	}
	meta := xdr.TransactionMeta{
		V: 3,
		V3: &xdr.TransactionMetaV3{
			SorobanMeta: &xdr.SorobanTransactionMeta{
				Events:      []xdr.ContractEvent{ce},
				ReturnValue: xdr.ScVal{Type: xdr.ScValTypeScvVoid},
			},
		},
	}
	metaB64, err := xdr.MarshalBase64(meta)
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"status":        "SUCCESS",
				"resultMetaXdr": metaB64,
			},
		})
	}))
	t.Cleanup(srv.Close)

	v := NewRPCChainEventVerifier(srv.URL)
	got, err := v.VerifyVaultEvent(context.Background(), "tx-1", contract, "withdraw")
	if err != nil {
		t.Fatalf("VerifyVaultEvent: %v", err)
	}
	want := decimal.RequireFromString("25")
	if !got.Amount.Equal(want) {
		t.Fatalf("amount = %s, want %s (event stroops, not a client body)", got.Amount, want)
	}
	if got.ContractID != contract {
		t.Fatalf("contract = %s, want %s", got.ContractID, contract)
	}
	if got.TxHash != "tx-1" {
		t.Fatalf("tx hash = %s, want tx-1", got.TxHash)
	}
}

func TestNormalizeEventType(t *testing.T) {
	if got := normalizeEventType("WITHDRAW"); got != "withdraw" {
		t.Fatalf("got %q", got)
	}
	if got := normalizeEventType("withdrawal"); got != "withdraw" {
		t.Fatalf("got %q", got)
	}
}
