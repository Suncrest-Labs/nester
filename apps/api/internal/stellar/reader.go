package stellar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stellar/go/keypair"
	"github.com/stellar/go/txnbuild"
	"github.com/stellar/go/xdr"
)

// ContractReader performs read-only Soroban contract simulations via RPC.
type ContractReader struct {
	rpcURL            string
	networkPassphrase string
	sourceAddress     string
	httpClient        *http.Client
}

// NewContractReader builds a reader that simulates view calls without submitting
// transactions. sourceAddress is used as the transaction source for simulation.
func NewContractReader(rpcURL, networkPassphrase, sourceAddress string) *ContractReader {
	if sourceAddress == "" {
		sourceAddress = keypair.MustRandom().Address()
	}
	return &ContractReader{
		rpcURL:            rpcURL,
		networkPassphrase: networkPassphrase,
		sourceAddress:     sourceAddress,
		httpClient:        &http.Client{Timeout: 30 * time.Second},
	}
}

// TotalAssets calls the vault_token total_assets() view and converts the i128
// return value (7-decimal stroops) to a decimal USDC amount.
func (r *ContractReader) TotalAssets(ctx context.Context, contractAddress string) (decimal.Decimal, error) {
	return r.VaultBalance(ctx, contractAddress)
}

// VaultBalance satisfies performance.BalanceProvider.
func (r *ContractReader) VaultBalance(ctx context.Context, contractAddress string) (decimal.Decimal, error) {
	raw, err := r.simulateI128(ctx, contractAddress, "total_assets", nil)
	if err != nil {
		return decimal.Zero, err
	}
	// Soroban vault amounts are stored in 7-decimal stroops (Stellar standard).
	return decimal.NewFromInt(raw).Shift(-7), nil
}

// SourceAPYBPS calls yield_registry get_source_performance(id) and returns
// current_apy_bps from the on-chain record.
func (r *ContractReader) SourceAPYBPS(ctx context.Context, registryAddress, protocolID string) (uint32, error) {
	contractScAddr, err := contractAddressToXDR(registryAddress)
	if err != nil {
		return 0, err
	}

	symbol := xdr.ScSymbol(protocolID)
	args := []xdr.ScVal{{
		Type: xdr.ScValTypeScvSymbol,
		Sym:  &symbol,
	}}

	val, err := r.simulate(ctx, contractScAddr, "get_source_performance", args)
	if err != nil {
		return 0, err
	}
	if val.Type != xdr.ScValTypeScvMap {
		return 0, fmt.Errorf("unexpected get_source_performance return type")
	}

	scMap := val.MustMap()
	for _, entry := range *scMap {
		if entry.Key.Type == xdr.ScValTypeScvSymbol && string(entry.Key.MustSym()) == "current_apy_bps" {
			if entry.Val.Type == xdr.ScValTypeScvU32 {
				return uint32(entry.Val.MustU32()), nil
			}
		}
	}
	return 0, fmt.Errorf("current_apy_bps not found in performance response")
}

func (r *ContractReader) simulateI128(ctx context.Context, contractAddress, functionName string, args []xdr.ScVal) (int64, error) {
	contractScAddr, err := contractAddressToXDR(contractAddress)
	if err != nil {
		return 0, err
	}
	val, err := r.simulate(ctx, contractScAddr, functionName, args)
	if err != nil {
		return 0, err
	}
	if val.Type != xdr.ScValTypeScvI128 {
		return 0, fmt.Errorf("expected i128 return from %s", functionName)
	}
	parts := val.MustI128()
	hi := int64(parts.Hi)
	lo := int64(parts.Lo)
	if hi < 0 {
		return (hi << 64) | int64(uint64(lo)), nil
	}
	return (hi << 64) + lo, nil
}

func (r *ContractReader) simulate(ctx context.Context, contractScAddr xdr.ScAddress, functionName string, args []xdr.ScVal) (xdr.ScVal, error) {
	hostFn := xdr.HostFunction{
		Type: xdr.HostFunctionTypeHostFunctionTypeInvokeContract,
		InvokeContract: &xdr.InvokeContractArgs{
			ContractAddress: contractScAddr,
			FunctionName:    xdr.ScSymbol(functionName),
			Args:            args,
		},
	}

	sourceAccount := txnbuild.NewSimpleAccount(r.sourceAddress, 1)
	tx, err := txnbuild.NewTransaction(txnbuild.TransactionParams{
		SourceAccount:        &sourceAccount,
		IncrementSequenceNum: true,
		Operations:           []txnbuild.Operation{&txnbuild.InvokeHostFunction{HostFunction: hostFn}},
		BaseFee:              txnbuild.MinBaseFee,
		Preconditions:        txnbuild.Preconditions{TimeBounds: txnbuild.NewTimeout(300)},
	})
	if err != nil {
		return xdr.ScVal{}, fmt.Errorf("build simulate tx: %w", err)
	}

	txB64, err := tx.Base64()
	if err != nil {
		return xdr.ScVal{}, fmt.Errorf("encode simulate tx: %w", err)
	}

	var resp rpcResponse[simulateResultExtended]
	if err := r.rpcCall(ctx, "simulateTransaction", simulateParams{Transaction: txB64}, &resp); err != nil {
		return xdr.ScVal{}, err
	}
	if resp.Error != nil {
		return xdr.ScVal{}, fmt.Errorf("%w: %s", ErrSimulateFailed, resp.Error.Message)
	}
	if resp.Result.Error != "" {
		return xdr.ScVal{}, fmt.Errorf("%w: %s", ErrSimulateFailed, resp.Result.Error)
	}
	if resp.Result.ReturnValue == "" {
		return xdr.ScVal{}, fmt.Errorf("%w: empty return value from %s", ErrSimulateFailed, functionName)
	}

	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(resp.Result.ReturnValue, &val); err != nil {
		return xdr.ScVal{}, fmt.Errorf("decode return value: %w", err)
	}
	return val, nil
}

type simulateResultExtended struct {
	TransactionData string `json:"transactionData"`
	MinResourceFee  string `json:"minResourceFee"`
	Error           string `json:"error,omitempty"`
	ReturnValue     string `json:"returnValue,omitempty"`
}

func (r *ContractReader) rpcCall(ctx context.Context, method string, params, result any) error {
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.rpcURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("rpc %s: %w", method, err)
	}
	defer resp.Body.Close()

	return json.NewDecoder(resp.Body).Decode(result)
}
