package stellar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stellar/go/strkey"
	"github.com/stellar/go/xdr"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

const stroopsPerUnit int64 = 10_000_000

// VerifiedVaultEvent is a vault contract event taken from a confirmed
// on-chain transaction. Amount is in display units (stroops / 1e7), matching
// the vault ledger — never the client-supplied request body (nester#1076).
type VerifiedVaultEvent struct {
	TxHash     string
	EventType  string
	Amount     decimal.Decimal
	ContractID string
}

// ChainEventVerifier looks up a transaction hash and returns the matching
// vault contract event. Shared by deposit and withdrawal recording so both
// money paths reconcile against the same on-chain source of truth.
type ChainEventVerifier interface {
	VerifyVaultEvent(ctx context.Context, txHash, contractID, eventType string) (VerifiedVaultEvent, error)
}

// RPCChainEventVerifier implements ChainEventVerifier against a Soroban RPC
// getTransaction endpoint. It requires status=SUCCESS and a contract event
// on the given vault whose topic matches eventType.
type RPCChainEventVerifier struct {
	rpcURL string
	client *http.Client
}

func NewRPCChainEventVerifier(rpcURL string) *RPCChainEventVerifier {
	return &RPCChainEventVerifier{
		rpcURL: strings.TrimSpace(rpcURL),
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

type rpcGetTxResult struct {
	Status        string `json:"status"`
	ResultMetaXdr string `json:"resultMetaXdr"`
}

func (v *RPCChainEventVerifier) VerifyVaultEvent(
	ctx context.Context,
	txHash, contractID, eventType string,
) (VerifiedVaultEvent, error) {
	txHash = strings.TrimSpace(txHash)
	contractID = strings.TrimSpace(contractID)
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	if txHash == "" {
		return VerifiedVaultEvent{}, vault.ErrTxHashRequired
	}
	if v.rpcURL == "" {
		return VerifiedVaultEvent{}, vault.ErrUnverifiedChainTx
	}

	var resp rpcResponse[rpcGetTxResult]
	if err := v.rpcCall(ctx, "getTransaction", getTxParams{Hash: txHash}, &resp); err != nil {
		return VerifiedVaultEvent{}, fmt.Errorf("%w: %v", vault.ErrUnverifiedChainTx, err)
	}
	if resp.Error != nil {
		return VerifiedVaultEvent{}, fmt.Errorf("%w: %s", vault.ErrUnverifiedChainTx, resp.Error.Message)
	}

	switch strings.ToUpper(resp.Result.Status) {
	case "SUCCESS":
		// continue
	case "NOT_FOUND", "":
		return VerifiedVaultEvent{}, fmt.Errorf("%w: transaction not found", vault.ErrUnverifiedChainTx)
	default:
		return VerifiedVaultEvent{}, fmt.Errorf("%w: transaction status %s", vault.ErrUnverifiedChainTx, resp.Result.Status)
	}

	events, err := parseContractEventsFromMeta(resp.Result.ResultMetaXdr)
	if err != nil {
		return VerifiedVaultEvent{}, fmt.Errorf("%w: %v", vault.ErrUnverifiedChainTx, err)
	}

	wantType := normalizeEventType(eventType)
	for _, ev := range events {
		if contractID != "" && ev.ContractID != contractID {
			continue
		}
		if normalizeEventType(ev.EventType) != wantType {
			continue
		}
		if ev.Amount.Cmp(decimal.Zero) <= 0 {
			continue
		}
		ev.TxHash = txHash
		return ev, nil
	}

	return VerifiedVaultEvent{}, fmt.Errorf("%w: no %s event for contract %s", vault.ErrUnverifiedChainTx, eventType, contractID)
}

func (v *RPCChainEventVerifier) rpcCall(ctx context.Context, method string, params, result any) error {
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.rpcURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("rpc returned %d: %s", resp.StatusCode, string(payload))
	}
	return json.NewDecoder(resp.Body).Decode(result)
}

func parseContractEventsFromMeta(metaB64 string) ([]VerifiedVaultEvent, error) {
	metaB64 = strings.TrimSpace(metaB64)
	if metaB64 == "" {
		return nil, errors.New("missing resultMetaXdr")
	}
	var meta xdr.TransactionMeta
	if err := xdr.SafeUnmarshalBase64(metaB64, &meta); err != nil {
		return nil, fmt.Errorf("decode resultMetaXdr: %w", err)
	}

	var out []VerifiedVaultEvent
	for _, ce := range collectContractEvents(meta) {
		ev, ok := verifiedEventFromXDR(ce)
		if ok {
			out = append(out, ev)
		}
	}
	return out, nil
}

func collectContractEvents(meta xdr.TransactionMeta) []xdr.ContractEvent {
	events := make([]xdr.ContractEvent, 0)
	if v3 := meta.V3; v3 != nil && v3.SorobanMeta != nil {
		events = append(events, v3.SorobanMeta.Events...)
	}
	if v4, err := meta.V4(); err == nil && v4 != nil && v4.SorobanMeta != nil {
		events = append(events, v4.SorobanMeta.Events...)
	}
	return events
}

func verifiedEventFromXDR(ce xdr.ContractEvent) (VerifiedVaultEvent, bool) {
	if ce.ContractId == nil {
		return VerifiedVaultEvent{}, false
	}
	encoded, err := strkey.Encode(strkey.VersionByteContract, (*ce.ContractId)[:])
	if err != nil {
		return VerifiedVaultEvent{}, false
	}

	body, ok := ce.Body.GetV0()
	if !ok {
		return VerifiedVaultEvent{}, false
	}

	eventType := ""
	if len(body.Topics) > 0 {
		eventType = scValSymbol(body.Topics[0])
	}
	if eventType == "" && len(body.Topics) > 1 {
		eventType = scValSymbol(body.Topics[1])
	}
	amount, ok := scValAmount(body.Data)
	if !ok || amount.Cmp(decimal.Zero) <= 0 {
		return VerifiedVaultEvent{}, false
	}

	return VerifiedVaultEvent{
		EventType:  eventType,
		Amount:     stroopsToDisplay(amount),
		ContractID: encoded,
	}, true
}

func scValSymbol(val xdr.ScVal) string {
	switch val.Type {
	case xdr.ScValTypeScvSymbol:
		if val.Sym != nil {
			return string(*val.Sym)
		}
	case xdr.ScValTypeScvString:
		if val.Str != nil {
			return string(*val.Str)
		}
	}
	return ""
}

func scValAmount(val xdr.ScVal) (decimal.Decimal, bool) {
	switch val.Type {
	case xdr.ScValTypeScvI128:
		if val.I128 == nil {
			return decimal.Zero, false
		}
		return i128PartsToDecimal(*val.I128)
	case xdr.ScValTypeScvU64:
		if val.U64 == nil {
			return decimal.Zero, false
		}
		return decimal.NewFromUint64(uint64(*val.U64)), true
	case xdr.ScValTypeScvI64:
		if val.I64 == nil {
			return decimal.Zero, false
		}
		return decimal.NewFromInt(int64(*val.I64)), true
	case xdr.ScValTypeScvMap:
		if val.Map == nil || *val.Map == nil {
			return decimal.Zero, false
		}
		for _, entry := range **val.Map {
			name := scValSymbol(entry.Key)
			if name != "amount" && name != "value" {
				continue
			}
			return scValAmount(entry.Val)
		}
	}
	return decimal.Zero, false
}

func i128PartsToDecimal(parts xdr.Int128Parts) (decimal.Decimal, bool) {
	if parts.Hi != 0 {
		return decimal.Zero, false
	}
	return decimal.NewFromUint64(uint64(parts.Lo)), true
}

func stroopsToDisplay(stroops decimal.Decimal) decimal.Decimal {
	return stroops.Div(decimal.NewFromInt(stroopsPerUnit))
}

func normalizeEventType(eventType string) string {
	s := strings.ToLower(strings.TrimSpace(eventType))
	switch s {
	case "withdraw", "withdrawal", "withdrawn":
		return "withdraw"
	case "deposit", "deposited":
		return "deposit"
	case "harvest", "harvested", "yield_harvest":
		return "harvest"
	case "rebalance", "rebal_cmp":
		return "rebalance"
	default:
		return s
	}
}
