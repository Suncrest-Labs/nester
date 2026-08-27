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
	// Account is the address the event was emitted for, read from the event
	// topics. Empty when the contract does not include one. Callers use it to
	// confirm the event belongs to the requesting user rather than to anyone
	// who happens to share the contract (nester#1076).
	Account string
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
	if v4 := meta.V4; v4 != nil {
		for _, te := range v4.Events {
			events = append(events, te.Event)
		}
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

	// collectContractEvents appends the whole event stream, which carries
	// system and diagnostic events too. Only a genuine contract event is
	// authoritative for a balance change.
	if ce.Type != xdr.ContractEventTypeContract {
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

	// The vault contracts emit (event_name, account) as topics. Scan for the
	// first topic that decodes as an address.
	account := ""
	for _, topic := range body.Topics {
		if addr, ok := scValAddress(topic); ok {
			account = addr
			break
		}
	}

	return VerifiedVaultEvent{
		EventType:  eventType,
		Amount:     stroopsToDisplay(amount),
		ContractID: encoded,
		Account:    account,
	}, true
}

// scValAddress renders an ScVal address as a strkey, when it is one.
func scValAddress(val xdr.ScVal) (string, bool) {
	addr, ok := val.GetAddress()
	if !ok {
		return "", false
	}
	encoded, err := addr.String()
	if err != nil {
		return "", false
	}
	return encoded, true
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

// i128PartsToDecimal reconstructs the full signed 128-bit value.
//
// Rejecting everything with a non-zero Hi word dropped every negative value
// AND every amount at or above 2^63 stroops, so a genuinely successful
// transaction was reported unverifiable: funds left the contract and the
// balance was never debited.
func i128PartsToDecimal(parts xdr.Int128Parts) (decimal.Decimal, bool) {
	hi := decimal.NewFromInt(int64(parts.Hi))
	lo := decimal.NewFromUint64(uint64(parts.Lo))
	// value = hi * 2^64 + lo
	shift := decimal.NewFromInt(2).Pow(decimal.NewFromInt(64))
	return hi.Mul(shift).Add(lo), true
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
