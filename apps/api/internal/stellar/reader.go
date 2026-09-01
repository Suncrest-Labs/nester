package stellar

import (
	"context"
	"fmt"
	"math"
	"net/http"

	"github.com/shopspring/decimal"
	"github.com/stellar/go/keypair"
	"github.com/stellar/go/txnbuild"
	"github.com/stellar/go/xdr"
)

// ContractReader performs read-only Soroban contract simulations via RPC.
//
// Every call it makes is a simulation — it never submits — so all of its
// traffic is idempotent and eligible for the shared retry policy.
type ContractReader struct {
	rpcURL            string
	networkPassphrase string
	sourceAddress     string
	httpClient        *http.Client
	rpcOpts           RPCOptions
	rpc               *rpcClient
}

// NewContractReader builds a reader that simulates view calls without submitting
// transactions. sourceAddress is used as the transaction source for simulation.
func NewContractReader(rpcURL, networkPassphrase, sourceAddress string) *ContractReader {
	if sourceAddress == "" {
		sourceAddress = keypair.MustRandom().Address()
	}
	r := &ContractReader{
		rpcURL:            rpcURL,
		networkPassphrase: networkPassphrase,
		sourceAddress:     sourceAddress,
		httpClient:        &http.Client{Timeout: defaultRPCTimeout},
	}
	r.rebuildRPC()
	return r
}

// SetHTTPClient replaces the HTTP client used for outbound calls. It exists so
// startup can install a metrics-instrumented, circuit-broken transport; a nil
// client is ignored so callers need not branch.
func (r *ContractReader) SetHTTPClient(client *http.Client) {
	if client != nil {
		r.httpClient = client
		r.rebuildRPC()
	}
}

// SetRPCOptions installs the shared retry policy and its metrics observer.
// Startup calls it; without it the reader retries on the package defaults,
// which keeps tests and tooling working unchanged.
func (r *ContractReader) SetRPCOptions(opts RPCOptions) {
	r.rpcOpts = opts
	r.rebuildRPC()
}

// rebuildRPC recreates the shared caller after any of its inputs change. The
// caller is immutable once built, so replacing it is simpler than mutating it
// under a lock — and these setters only ever run during startup wiring.
func (r *ContractReader) rebuildRPC() {
	r.rpc = newRPCClient(r.rpcURL, r.httpClient, r.rpcOpts, false)
}

// TotalAssets calls the vault_token total_assets() view and converts the i128
// return value (7-decimal stroops) to a decimal USDC amount.
//
// NOT for reconciliation: this method returns DISPLAY units, and *ContractReader
// therefore satisfies reconciliation.VaultBalanceReader with the wrong unit.
// Reconciliation compares against vaults.current_balance, which stores raw
// stroops — wire StroopsBalanceReader there, never this reader directly, or
// every vault diverges by a factor of 1e7.
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

// TotalAssetsStroops calls the vault_token total_assets() view and returns the
// i128 value as raw stroops, without the display rescale TotalAssets applies.
//
// This is the reconciliation read (nester#1082). The event indexer stores
// vault balances "as emitted" — raw stroop integers, not display USDC (see
// docs/event-indexer-replay.md fixture rule 7 and migration 103, which widened
// vaults.current_balance specifically so i128 stroop amounts round-trip
// exactly). Comparing that column against the chain therefore has to happen in
// stroops: rescaling either side would turn an exact integer comparison into a
// decimal one and hide single-stroop bookkeeping errors — the exact class of
// divergence reconciliation exists to catch.
//
// Known bound: simulateI128 rejects values outside int64 (~9.2e18 stroops,
// ~922 billion USDC). A vault past that errors here rather than reporting a
// truncated balance; the balance comparator logs and skips it each pass, so
// the bound is visible in the logs, never silently wrong.
func (r *ContractReader) TotalAssetsStroops(ctx context.Context, contractAddress string) (decimal.Decimal, error) {
	raw, err := r.simulateI128(ctx, contractAddress, "total_assets", nil)
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromInt(raw), nil
}

// StroopsBalanceReader adapts ContractReader to the reconciliation package's
// VaultBalanceReader interface (TotalAssets) while keeping the raw-stroops
// unit contract documented on TotalAssetsStroops. The reconciliation engine
// asks for "total assets" and must receive the same unit the database stores.
type StroopsBalanceReader struct {
	Reader *ContractReader
}

func (s StroopsBalanceReader) TotalAssets(ctx context.Context, contractAddress string) (decimal.Decimal, error) {
	return s.Reader.TotalAssetsStroops(ctx, contractAddress)
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
	lo := uint64(parts.Lo)
	// The i128 value is expected to fit in int64 (e.g. stroops).
	// hi==0: positive value in lo; hi==-1: negative value, lo holds two's complement.
	if hi == 0 {
		// hi==0 means the value is positive, but lo may still exceed int64
		// max; converting without this check would wrap it negative
		// (nester#1035, G115).
		if lo > math.MaxInt64 {
			return 0, fmt.Errorf("i128 value from %s overflows int64", functionName)
		}
		return int64(lo), nil
	}
	if hi == -1 {
		// Two's complement negative: lo must be at or above the representable
		// minimum for the result to fit.
		if lo < uint64(1<<63) {
			return 0, fmt.Errorf("i128 value from %s underflows int64", functionName)
		}
		return int64(lo), nil // #nosec G115 -- bounded above: lo >= 2^63 makes this a valid negative int64
	}
	return 0, fmt.Errorf("i128 value from %s overflows int64 (hi=%d)", functionName, hi)
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

// rpcCall delegates to the shared client so reads here get the same bounded,
// jittered retry every other Soroban call site does (nester#1086).
func (r *ContractReader) rpcCall(ctx context.Context, method string, params, result any) error {
	return r.rpc.call(ctx, method, params, result)
}
