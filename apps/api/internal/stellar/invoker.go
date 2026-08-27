package stellar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/stellar/go/strkey"
	"github.com/stellar/go/txnbuild"
	"github.com/stellar/go/xdr"

	"github.com/suncrestlabs/nester/apps/api/internal/telemetry"
)

var (
	ErrSimulateFailed  = errors.New("soroban simulate failed")
	ErrSubmitFailed    = errors.New("soroban send failed")
	ErrTxFailed        = errors.New("soroban transaction failed")
	ErrInvalidContract = errors.New("invalid contract address")
)

// ContractInvoker submits InvokeHostFunction transactions to a Soroban RPC node.
type ContractInvoker struct {
	rpcURL            string
	horizonURL        string
	networkPassphrase string
	// signer applies the operator signature. The invoker builds and simulates
	// transactions but never holds key material itself — see signer.go and
	// docs/security/signing-isolation.md.
	signer          TransactionSigner
	operatorAddress string
	httpClient      *http.Client
}

// NewContractInvoker builds an invoker whose operator key lives in this
// process. Retained for local development and for deployments that have not
// split out the signer; NewContractInvokerWithSigner is the isolated form.
func NewContractInvoker(rpcURL, horizonURL, networkPassphrase, operatorSecret string) (*ContractInvoker, error) {
	signer, err := NewLocalSigner(operatorSecret, networkPassphrase)
	if err != nil {
		return nil, err
	}
	return NewContractInvokerWithSigner(rpcURL, horizonURL, networkPassphrase, signer)
}

// NewContractInvokerWithSigner builds an invoker that delegates signing to the
// supplied signer. When that signer is a remote one, this process holds no
// operator key material at all.
//
// A nil signer is permitted and yields a read-only invoker: simulation and
// query paths work, and any signing attempt fails with ErrNoSigner. That is the
// correct configuration for deployments that only read chain state.
func NewContractInvokerWithSigner(rpcURL, horizonURL, networkPassphrase string, signer TransactionSigner) (*ContractInvoker, error) {
	inv := &ContractInvoker{
		rpcURL:            rpcURL,
		horizonURL:        horizonURL,
		networkPassphrase: networkPassphrase,
		signer:            signer,
		httpClient:        &http.Client{Timeout: 30 * time.Second},
	}
	if signer != nil {
		inv.operatorAddress = signer.OperatorAddress()
	}
	return inv, nil
}

// requireOperatorAddress returns the address transactions are built against,
// or an error when no signer is configured.
func (c *ContractInvoker) requireOperatorAddress() (string, error) {
	if c.signer == nil || c.operatorAddress == "" {
		return "", ErrNoSigner
	}
	return c.operatorAddress, nil
}

// signEnvelope delegates to the configured signer, guarding the nil case
// locally rather than relying on an earlier call in the same function having
// already checked. Each signing path is then safe on its own terms, so
// reordering the code above it cannot silently reintroduce a nil dereference.
func (c *ContractInvoker) signEnvelope(ctx context.Context, req SignRequest) (string, error) {
	if c.signer == nil {
		return "", ErrNoSigner
	}
	return c.signer.SignEnvelope(ctx, req)
}

// SetHTTPClient replaces the HTTP client used for outbound calls. It exists so
// startup can install a metrics-instrumented transport; a nil client is
// ignored so callers need not branch.
func (c *ContractInvoker) SetHTTPClient(client *http.Client) {
	if client != nil {
		c.httpClient = client
	}
}

// InvokeVoidFunction calls a contract function with signature (caller: Address).
func (c *ContractInvoker) InvokeVoidFunction(ctx context.Context, contractAddress, functionName string) error {
	ctx, span := startContractSpan(ctx, "invoke", contractAddress, functionName)
	defer span.End()

	hash, err := c.InvokeVoidFunctionSubmit(ctx, contractAddress, functionName)
	if err != nil {
		telemetry.RecordError(span, err)
		return err
	}
	recordTxHash(span, hash)

	if err := c.waitForTx(ctx, hash); err != nil {
		telemetry.RecordError(span, err)
		return err
	}
	return nil
}

// SimulateVoidFunction dry-runs a (caller: Address) contract call without submitting.
func (c *ContractInvoker) SimulateVoidFunction(ctx context.Context, contractAddress, functionName string) error {
	ctx, span := startContractSpan(ctx, "simulate", contractAddress, functionName)
	defer span.End()

	txB64, err := c.buildUnsignedVoidInvoke(ctx, contractAddress, functionName)
	if err != nil {
		telemetry.RecordError(span, err)
		return err
	}
	if _, err = c.simulate(ctx, txB64); err != nil {
		telemetry.RecordError(span, err)
		return err
	}
	return nil
}

// InvokeVoidFunctionSubmit simulates, signs, and submits a void contract call.
// Returns the transaction hash without waiting for ledger confirmation.
func (c *ContractInvoker) InvokeVoidFunctionSubmit(ctx context.Context, contractAddress, functionName string) (string, error) {
	signedB64, err := c.signVoidInvoke(ctx, contractAddress, functionName)
	if err != nil {
		return "", err
	}
	return c.send(ctx, signedB64)
}

func (c *ContractInvoker) buildUnsignedVoidInvoke(ctx context.Context, contractAddress, functionName string) (string, error) {
	contractScAddr, err := contractAddressToXDR(contractAddress)
	if err != nil {
		return "", err
	}

	operatorAddr, err := c.requireOperatorAddress()
	if err != nil {
		return "", err
	}
	callerScAddr, err := accountAddressToXDR(operatorAddr)
	if err != nil {
		return "", err
	}

	hostFn := xdr.HostFunction{
		Type: xdr.HostFunctionTypeHostFunctionTypeInvokeContract,
		InvokeContract: &xdr.InvokeContractArgs{
			ContractAddress: contractScAddr,
			FunctionName:    xdr.ScSymbol(functionName),
			Args: []xdr.ScVal{
				{
					Type:    xdr.ScValTypeScvAddress,
					Address: &callerScAddr,
				},
			},
		},
	}

	seq, err := c.getSequenceNumber(ctx)
	if err != nil {
		return "", fmt.Errorf("get sequence number: %w", err)
	}

	sourceAccount := txnbuild.NewSimpleAccount(operatorAddr, seq)

	tx, err := txnbuild.NewTransaction(txnbuild.TransactionParams{
		SourceAccount:        &sourceAccount,
		IncrementSequenceNum: true,
		Operations: []txnbuild.Operation{
			&txnbuild.InvokeHostFunction{
				HostFunction: hostFn,
			},
		},
		BaseFee:       txnbuild.MinBaseFee,
		Preconditions: txnbuild.Preconditions{TimeBounds: txnbuild.NewTimeout(int64((5 * time.Minute).Seconds()))},
	})
	if err != nil {
		return "", fmt.Errorf("build transaction: %w", err)
	}

	return tx.Base64()
}

func (c *ContractInvoker) signVoidInvoke(ctx context.Context, contractAddress, functionName string) (string, error) {
	txB64, err := c.buildUnsignedVoidInvoke(ctx, contractAddress, functionName)
	if err != nil {
		return "", err
	}

	simResult, err := c.simulate(ctx, txB64)
	if err != nil {
		return "", err
	}

	generic, err := txnbuild.TransactionFromXDR(txB64)
	if err != nil {
		return "", fmt.Errorf("parse tx: %w", err)
	}
	tx, ok := generic.Transaction()
	if !ok {
		return "", errors.New("expected a transaction, got fee-bump")
	}

	var sorobanData xdr.SorobanTransactionData
	if err := xdr.SafeUnmarshalBase64(simResult.TransactionData, &sorobanData); err != nil {
		return "", fmt.Errorf("decode soroban data: %w", err)
	}

	envelope := tx.ToXDR()
	envelope.V1.Tx.Ext = xdr.TransactionExt{
		V:           1,
		SorobanData: &sorobanData,
	}
	minFee, err := strconv.ParseInt(simResult.MinResourceFee, 10, 64)
	if err != nil {
		return "", fmt.Errorf("parse simulation min resource fee %q: %w", simResult.MinResourceFee, err)
	}
	envelope.V1.Tx.Fee = xdr.Uint32(txnbuild.MinBaseFee + minFee)

	envB64, err := xdr.MarshalBase64(envelope)
	if err != nil {
		return "", fmt.Errorf("encode patched envelope: %w", err)
	}

	generic, err = txnbuild.TransactionFromXDR(envB64)
	if err != nil {
		return "", fmt.Errorf("parse patched tx: %w", err)
	}

	inner, ok := generic.Transaction()
	if !ok {
		return "", errors.New("expected a transaction, got fee-bump")
	}

	// Signing is delegated across the signer boundary. The envelope is fully
	// built and simulated at this point; the signer re-validates the declared
	// intent against policy before applying the key.
	envelopeB64, err := inner.Base64()
	if err != nil {
		return "", fmt.Errorf("encode transaction for signing: %w", err)
	}
	return c.signEnvelope(ctx, SignRequest{
		EnvelopeXDR:     envelopeB64,
		Operation:       functionName,
		ContractAddress: contractAddress,
	})
}

// ── JSON-RPC helpers ──────────────────────────────────────────────────────────

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type simulateParams struct {
	Transaction string `json:"transaction"`
}

type simulateResult struct {
	TransactionData string `json:"transactionData"`
	MinResourceFee  string `json:"minResourceFee"`
	Error           string `json:"error,omitempty"`
	Results         []struct {
		XDR string `json:"xdr"`
	} `json:"results,omitempty"`
}

type sendParams struct {
	Transaction string `json:"transaction"`
}

type sendResult struct {
	Hash           string `json:"hash"`
	Status         string `json:"status"`
	ErrorResultXDR string `json:"errorResultXdr,omitempty"`
}

type getTxParams struct {
	Hash string `json:"hash"`
}

type getTxResult struct {
	Status string `json:"status"`
}

type rpcResponse[T any] struct {
	Result T `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *ContractInvoker) rpcCall(ctx context.Context, method string, params, result any) error {
	// One span per JSON-RPC round trip so the trace waterfall separates
	// simulate, submit, and each poll of getTransaction. Neither params nor
	// the response body is recorded: both carry transaction XDR.
	ctx, span := startRPCSpan(ctx, method)
	defer span.End()

	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		telemetry.RecordError(span, err)
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rpcURL, bytes.NewReader(body))
	if err != nil {
		telemetry.RecordError(span, err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		wrapped := fmt.Errorf("rpc %s: %w", method, err)
		telemetry.RecordError(span, wrapped)
		return wrapped
	}
	defer resp.Body.Close()

	span.SetAttributes(semconv.HTTPResponseStatusCode(resp.StatusCode))

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		telemetry.RecordError(span, err)
		return err
	}
	return nil
}

func (c *ContractInvoker) simulate(ctx context.Context, txB64 string) (simulateResult, error) {
	var resp rpcResponse[simulateResult]
	if err := c.rpcCall(ctx, "simulateTransaction", simulateParams{Transaction: txB64}, &resp); err != nil {
		return simulateResult{}, err
	}
	if resp.Error != nil {
		return simulateResult{}, fmt.Errorf("%w: %s", ErrSimulateFailed, resp.Error.Message)
	}
	if resp.Result.Error != "" {
		return simulateResult{}, fmt.Errorf("%w: %s", ErrSimulateFailed, resp.Result.Error)
	}
	return resp.Result, nil
}

func (c *ContractInvoker) send(ctx context.Context, txB64 string) (string, error) {
	var resp rpcResponse[sendResult]
	if err := c.rpcCall(ctx, "sendTransaction", sendParams{Transaction: txB64}, &resp); err != nil {
		return "", err
	}
	if resp.Error != nil {
		return "", fmt.Errorf("%w: %s", ErrSubmitFailed, resp.Error.Message)
	}
	if resp.Result.Status == "ERROR" {
		return "", fmt.Errorf("%w: %s", ErrSubmitFailed, resp.Result.ErrorResultXDR)
	}
	return resp.Result.Hash, nil
}

func (c *ContractInvoker) waitForTx(ctx context.Context, hash string) error {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			var resp rpcResponse[getTxResult]
			if err := c.rpcCall(ctx, "getTransaction", getTxParams{Hash: hash}, &resp); err != nil {
				return err
			}
			if resp.Error != nil {
				return fmt.Errorf("getTransaction: %s", resp.Error.Message)
			}
			recordTxStatus(trace.SpanFromContext(ctx), resp.Result.Status)

			switch resp.Result.Status {
			case "SUCCESS":
				return nil
			case "FAILED":
				return fmt.Errorf("%w: hash %s", ErrTxFailed, hash)
				// "NOT_FOUND" means still pending — keep polling
			}
		}
	}
}

// ── Horizon: account sequence number ─────────────────────────────────────────

func (c *ContractInvoker) getSequenceNumber(ctx context.Context) (int64, error) {
	operatorAddr, err := c.requireOperatorAddress()
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.horizonURL+"/accounts/"+operatorAddr, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("horizon getAccount: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		Sequence string `json:"sequence"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("decode account response: %w", err)
	}
	seq, err := strconv.ParseInt(body.Sequence, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse sequence %q: %w", body.Sequence, err)
	}
	return seq, nil
}

// PreviewWithdrawNet simulates withdrawal_fee_preview and returns the net
// assets the user would receive after fees (slippage-safe preview base).
func (c *ContractInvoker) PreviewWithdrawNet(ctx context.Context, contractAddress string, sharesStroops int64) (int64, error) {
	operatorAddr, err := c.requireOperatorAddress()
	if err != nil {
		return 0, err
	}
	callerScAddr, err := accountAddressToXDR(operatorAddr)
	if err != nil {
		return 0, err
	}

	result, err := c.simulateContractFn(ctx, contractAddress, "withdrawal_fee_preview", []xdr.ScVal{
		{Type: xdr.ScValTypeScvAddress, Address: &callerScAddr},
		int64ToI128ScVal(sharesStroops),
	})
	if err != nil {
		return 0, err
	}

	return scValMapFieldI128(result, "net_amount_received")
}

// SimulateI128Function simulates a contract call that takes a single i128
// argument and returns an i128.
func (c *ContractInvoker) SimulateI128Function(ctx context.Context, contractAddress, functionName string, arg int64) (int64, error) {
	result, err := c.simulateContractFn(ctx, contractAddress, functionName, []xdr.ScVal{
		int64ToI128ScVal(arg),
	})
	if err != nil {
		return 0, err
	}
	return i128ScValToInt64(result)
}

func (c *ContractInvoker) simulateContractFn(
	ctx context.Context,
	contractAddress, functionName string,
	args []xdr.ScVal,
) (xdr.ScVal, error) {
	contractScAddr, err := contractAddressToXDR(contractAddress)
	if err != nil {
		return xdr.ScVal{}, err
	}

	hostFn := xdr.HostFunction{
		Type: xdr.HostFunctionTypeHostFunctionTypeInvokeContract,
		InvokeContract: &xdr.InvokeContractArgs{
			ContractAddress: contractScAddr,
			FunctionName:    xdr.ScSymbol(functionName),
			Args:            args,
		},
	}

	operatorAddr, err := c.requireOperatorAddress()
	if err != nil {
		return xdr.ScVal{}, err
	}

	seq, err := c.getSequenceNumber(ctx)
	if err != nil {
		return xdr.ScVal{}, fmt.Errorf("get sequence number: %w", err)
	}

	sourceAccount := txnbuild.NewSimpleAccount(operatorAddr, seq)
	tx, err := txnbuild.NewTransaction(txnbuild.TransactionParams{
		SourceAccount:        &sourceAccount,
		IncrementSequenceNum: true,
		Operations: []txnbuild.Operation{
			&txnbuild.InvokeHostFunction{HostFunction: hostFn},
		},
		BaseFee:       txnbuild.MinBaseFee,
		Preconditions: txnbuild.Preconditions{TimeBounds: txnbuild.NewTimeout(int64((5 * time.Minute).Seconds()))},
	})
	if err != nil {
		return xdr.ScVal{}, fmt.Errorf("build transaction: %w", err)
	}

	txB64, err := tx.Base64()
	if err != nil {
		return xdr.ScVal{}, fmt.Errorf("encode transaction: %w", err)
	}

	simResult, err := c.simulate(ctx, txB64)
	if err != nil {
		return xdr.ScVal{}, err
	}
	if len(simResult.Results) == 0 || simResult.Results[0].XDR == "" {
		return xdr.ScVal{}, fmt.Errorf("%w: missing return value", ErrSimulateFailed)
	}

	var returnVal xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(simResult.Results[0].XDR, &returnVal); err != nil {
		return xdr.ScVal{}, fmt.Errorf("decode return value: %w", err)
	}
	return returnVal, nil
}

func scValMapFieldI128(val xdr.ScVal, fieldName string) (int64, error) {
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return 0, fmt.Errorf("expected struct map return value")
	}

	for _, entry := range **val.Map {
		sym, ok := scValAsSymbol(entry.Key)
		if !ok || sym != fieldName {
			continue
		}
		return i128ScValToInt64(entry.Val)
	}

	return 0, fmt.Errorf("field %q not found in preview result", fieldName)
}

func scValAsSymbol(val xdr.ScVal) (string, bool) {
	if val.Type != xdr.ScValTypeScvSymbol || val.Sym == nil {
		return "", false
	}
	return string(*val.Sym), true
}

// I128ScValToInt64 converts an i128 contract return value to int64, refusing
// any value that does not fit rather than truncating it.
//
// Truncation matters here: these values are stroop amounts on preview and
// balance paths, and a silently wrapped uint64 becomes a negative amount that
// downstream arithmetic would treat as real (nester#1035, G115).
func I128ScValToInt64(val xdr.ScVal) (int64, error) {
	return i128ScValToInt64(val)
}

func i128ScValToInt64(val xdr.ScVal) (int64, error) {
	if val.Type != xdr.ScValTypeScvI128 || val.I128 == nil {
		return 0, fmt.Errorf("expected i128 value")
	}
	if val.I128.Hi != 0 {
		if val.I128.Hi != -1 {
			return 0, fmt.Errorf("i128 value exceeds int64 range")
		}
		return 0, fmt.Errorf("negative asset amount")
	}
	if val.I128.Lo > xdr.Uint64(1<<63-1) {
		return 0, fmt.Errorf("i128 value exceeds int64 range")
	}
	return int64(val.I128.Lo), nil
}

// InvokeWithI128Pair calls a contract function with signature
// (caller: Address, arg0: i128, arg1: i128). Suitable for deposit and withdraw
// where the operator acts as the transaction source and user.
func (c *ContractInvoker) InvokeWithI128Pair(ctx context.Context, contractAddress, functionName string, arg0, arg1 int64) (string, error) {
	contractScAddr, err := contractAddressToXDR(contractAddress)
	if err != nil {
		return "", err
	}

	operatorAddr, err := c.requireOperatorAddress()
	if err != nil {
		return "", err
	}
	callerScAddr, err := accountAddressToXDR(operatorAddr)
	if err != nil {
		return "", err
	}

	hostFn := xdr.HostFunction{
		Type: xdr.HostFunctionTypeHostFunctionTypeInvokeContract,
		InvokeContract: &xdr.InvokeContractArgs{
			ContractAddress: contractScAddr,
			FunctionName:    xdr.ScSymbol(functionName),
			Args: []xdr.ScVal{
				{Type: xdr.ScValTypeScvAddress, Address: &callerScAddr},
				int64ToI128ScVal(arg0),
				int64ToI128ScVal(arg1),
			},
		},
	}

	seq, err := c.getSequenceNumber(ctx)
	if err != nil {
		return "", fmt.Errorf("get sequence number: %w", err)
	}

	sourceAccount := txnbuild.NewSimpleAccount(operatorAddr, seq)

	tx, err := txnbuild.NewTransaction(txnbuild.TransactionParams{
		SourceAccount:        &sourceAccount,
		IncrementSequenceNum: true,
		Operations: []txnbuild.Operation{
			&txnbuild.InvokeHostFunction{
				HostFunction: hostFn,
			},
		},
		BaseFee:       txnbuild.MinBaseFee,
		Preconditions: txnbuild.Preconditions{TimeBounds: txnbuild.NewTimeout(int64((5 * time.Minute).Seconds()))},
	})
	if err != nil {
		return "", fmt.Errorf("build transaction: %w", err)
	}

	txB64, err := tx.Base64()
	if err != nil {
		return "", fmt.Errorf("encode transaction: %w", err)
	}

	simResult, err := c.simulate(ctx, txB64)
	if err != nil {
		return "", err
	}

	var sorobanData xdr.SorobanTransactionData
	if err := xdr.SafeUnmarshalBase64(simResult.TransactionData, &sorobanData); err != nil {
		return "", fmt.Errorf("decode soroban data: %w", err)
	}

	envelope := tx.ToXDR()
	envelope.V1.Tx.Ext = xdr.TransactionExt{
		V:           1,
		SorobanData: &sorobanData,
	}
	minFee, err := strconv.ParseInt(simResult.MinResourceFee, 10, 64)
	if err != nil {
		return "", fmt.Errorf("parse simulation min resource fee %q: %w", simResult.MinResourceFee, err)
	}
	envelope.V1.Tx.Fee = xdr.Uint32(txnbuild.MinBaseFee + minFee)

	envB64, err := xdr.MarshalBase64(envelope)
	if err != nil {
		return "", fmt.Errorf("encode patched envelope: %w", err)
	}

	generic, err := txnbuild.TransactionFromXDR(envB64)
	if err != nil {
		return "", fmt.Errorf("parse patched tx: %w", err)
	}

	inner, ok := generic.Transaction()
	if !ok {
		return "", errors.New("expected a transaction, got fee-bump")
	}

	envelopeB64, err := inner.Base64()
	if err != nil {
		return "", fmt.Errorf("encode transaction for signing: %w", err)
	}
	signedB64, err := c.signEnvelope(ctx, SignRequest{
		EnvelopeXDR:     envelopeB64,
		Operation:       functionName,
		ContractAddress: contractAddress,
		Arg0:            arg0,
		Arg1:            arg1,
	})
	if err != nil {
		return "", err
	}

	hash, err := c.send(ctx, signedB64)
	if err != nil {
		return "", err
	}

	if err := c.waitForTx(ctx, hash); err != nil {
		return "", err
	}
	return hash, nil
}

// QueryWithI128Arg simulates a contract call with one i128 arg and returns the decoded XDR result.
// It is intended for read-only preview functions.
func (c *ContractInvoker) QueryWithI128Arg(ctx context.Context, contractAddress, functionName string, arg int64) (xdr.ScVal, error) {
	contractScAddr, err := contractAddressToXDR(contractAddress)
	if err != nil {
		return xdr.ScVal{}, err
	}

	hostFn := xdr.HostFunction{
		Type: xdr.HostFunctionTypeHostFunctionTypeInvokeContract,
		InvokeContract: &xdr.InvokeContractArgs{
			ContractAddress: contractScAddr,
			FunctionName:    xdr.ScSymbol(functionName),
			Args: []xdr.ScVal{
				int64ToI128ScVal(arg),
			},
		},
	}

	operatorAddr, err := c.requireOperatorAddress()
	if err != nil {
		return xdr.ScVal{}, err
	}

	seq, err := c.getSequenceNumber(ctx)
	if err != nil {
		return xdr.ScVal{}, fmt.Errorf("get sequence number: %w", err)
	}

	sourceAccount := txnbuild.NewSimpleAccount(operatorAddr, seq)

	tx, err := txnbuild.NewTransaction(txnbuild.TransactionParams{
		SourceAccount:        &sourceAccount,
		IncrementSequenceNum: true,
		Operations: []txnbuild.Operation{
			&txnbuild.InvokeHostFunction{
				HostFunction: hostFn,
			},
		},
		BaseFee:       txnbuild.MinBaseFee,
		Preconditions: txnbuild.Preconditions{TimeBounds: txnbuild.NewTimeout(int64((5 * time.Minute).Seconds()))},
	})
	if err != nil {
		return xdr.ScVal{}, fmt.Errorf("build transaction: %w", err)
	}

	txB64, err := tx.Base64()
	if err != nil {
		return xdr.ScVal{}, fmt.Errorf("encode transaction: %w", err)
	}

	simResult, err := c.simulate(ctx, txB64)
	if err != nil {
		return xdr.ScVal{}, err
	}

	if len(simResult.Results) == 0 {
		return xdr.ScVal{}, errors.New("simulation returned no results")
	}

	var parsed xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(simResult.Results[0].XDR, &parsed); err != nil {
		return xdr.ScVal{}, fmt.Errorf("decode result xdr: %w", err)
	}

	return parsed, nil
}

// AllocationWeightEntry is a single protocol weight for on-chain set_weights.
type AllocationWeightEntry struct {
	Protocol  string
	WeightBps uint32
}

// InvokeSetWeights calls allocation_strategy.set_weights(caller, weights).
func (c *ContractInvoker) InvokeSetWeights(ctx context.Context, contractAddress string, weights []AllocationWeightEntry) error {
	contractScAddr, err := contractAddressToXDR(contractAddress)
	if err != nil {
		return err
	}

	operatorAddr, err := c.requireOperatorAddress()
	if err != nil {
		return err
	}
	callerScAddr, err := accountAddressToXDR(operatorAddr)
	if err != nil {
		return err
	}

	weightVecItems := make([]xdr.ScVal, 0, len(weights))
	for _, w := range weights {
		bps := xdr.Uint32(w.WeightBps)
		sourceSym := xdr.ScSymbol(w.Protocol)
		mapEntries := []xdr.ScMapEntry{
			{
				Key: xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: scSymbol("source_id")},
				Val: xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &sourceSym},
			},
			{
				Key: xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: scSymbol("weight_bps")},
				Val: xdr.ScVal{Type: xdr.ScValTypeScvU32, U32: &bps},
			},
		}
		scMap := xdr.ScMap(mapEntries)
		mapPtr := &scMap
		weightVecItems = append(weightVecItems, xdr.ScVal{
			Type: xdr.ScValTypeScvMap,
			Map:  &mapPtr,
		})
	}
	scVec := xdr.ScVec(weightVecItems)
	vecPtr := &scVec

	hostFn := xdr.HostFunction{
		Type: xdr.HostFunctionTypeHostFunctionTypeInvokeContract,
		InvokeContract: &xdr.InvokeContractArgs{
			ContractAddress: contractScAddr,
			FunctionName:    xdr.ScSymbol("set_weights"),
			Args: []xdr.ScVal{
				{Type: xdr.ScValTypeScvAddress, Address: &callerScAddr},
				{Type: xdr.ScValTypeScvVec, Vec: &vecPtr},
			},
		},
	}

	return c.invokeHostFunction(ctx, hostFn)
}

// InvokeWithAddressAndBool calls a contract function with signature
// (user: Address, compound: bool). Returns the submitted transaction hash.
func (c *ContractInvoker) InvokeWithAddressAndBool(
	ctx context.Context,
	contractAddress, functionName, userAddress string,
	compound bool,
) (string, error) {
	contractScAddr, err := contractAddressToXDR(contractAddress)
	if err != nil {
		return "", err
	}

	userScAddr, err := accountAddressToXDR(userAddress)
	if err != nil {
		return "", err
	}

	boolVal := compound
	hostFn := xdr.HostFunction{
		Type: xdr.HostFunctionTypeHostFunctionTypeInvokeContract,
		InvokeContract: &xdr.InvokeContractArgs{
			ContractAddress: contractScAddr,
			FunctionName:    xdr.ScSymbol(functionName),
			Args: []xdr.ScVal{
				{Type: xdr.ScValTypeScvAddress, Address: &userScAddr},
				{Type: xdr.ScValTypeScvBool, B: &boolVal},
			},
		},
	}

	hash, err := c.submitHostFunction(ctx, hostFn)
	if err != nil {
		return "", err
	}
	if err := c.waitForTx(ctx, hash); err != nil {
		return hash, err
	}
	return hash, nil
}

func scSymbol(s string) *xdr.ScSymbol {
	v := xdr.ScSymbol(s)
	return &v
}

func (c *ContractInvoker) invokeHostFunction(ctx context.Context, hostFn xdr.HostFunction) error {
	hash, err := c.submitHostFunction(ctx, hostFn)
	if err != nil {
		return err
	}
	return c.waitForTx(ctx, hash)
}

func (c *ContractInvoker) submitHostFunction(ctx context.Context, hostFn xdr.HostFunction) (string, error) {
	operatorAddr, err := c.requireOperatorAddress()
	if err != nil {
		return "", err
	}

	seq, err := c.getSequenceNumber(ctx)
	if err != nil {
		return "", fmt.Errorf("get sequence number: %w", err)
	}

	sourceAccount := txnbuild.NewSimpleAccount(operatorAddr, seq)

	tx, err := txnbuild.NewTransaction(txnbuild.TransactionParams{
		SourceAccount:        &sourceAccount,
		IncrementSequenceNum: true,
		Operations: []txnbuild.Operation{
			&txnbuild.InvokeHostFunction{
				HostFunction: hostFn,
			},
		},
		BaseFee:       txnbuild.MinBaseFee,
		Preconditions: txnbuild.Preconditions{TimeBounds: txnbuild.NewTimeout(int64((5 * time.Minute).Seconds()))},
	})
	if err != nil {
		return "", fmt.Errorf("build transaction: %w", err)
	}

	txB64, err := tx.Base64()
	if err != nil {
		return "", fmt.Errorf("encode transaction: %w", err)
	}

	simResult, err := c.simulate(ctx, txB64)
	if err != nil {
		return "", err
	}

	var sorobanData xdr.SorobanTransactionData
	if err := xdr.SafeUnmarshalBase64(simResult.TransactionData, &sorobanData); err != nil {
		return "", fmt.Errorf("decode soroban data: %w", err)
	}

	envelope := tx.ToXDR()
	envelope.V1.Tx.Ext = xdr.TransactionExt{
		V:           1,
		SorobanData: &sorobanData,
	}
	minFee, err := strconv.ParseInt(simResult.MinResourceFee, 10, 64)
	if err != nil {
		return "", fmt.Errorf("parse simulation min resource fee %q: %w", simResult.MinResourceFee, err)
	}
	envelope.V1.Tx.Fee = xdr.Uint32(txnbuild.MinBaseFee + minFee)

	envB64, err := xdr.MarshalBase64(envelope)
	if err != nil {
		return "", fmt.Errorf("encode patched envelope: %w", err)
	}

	generic, err := txnbuild.TransactionFromXDR(envB64)
	if err != nil {
		return "", fmt.Errorf("parse patched tx: %w", err)
	}

	inner, ok := generic.Transaction()
	if !ok {
		return "", errors.New("expected a transaction, got fee-bump")
	}

	envelopeB64, err := inner.Base64()
	if err != nil {
		return "", fmt.Errorf("encode transaction for signing: %w", err)
	}
	signedB64, err := c.signEnvelope(ctx, SignRequest{
		EnvelopeXDR:     envelopeB64,
		Operation:       hostFunctionName(hostFn),
		ContractAddress: hostFunctionContract(hostFn),
	})
	if err != nil {
		return "", err
	}

	return c.send(ctx, signedB64)
}

func int64ToI128ScVal(n int64) xdr.ScVal {
	hi := xdr.Int64(0)
	lo := xdr.Uint64(uint64(n)) // #nosec G115 -- two's complement i128 encoding; hi is set to -1 for negatives
	if n < 0 {
		hi = xdr.Int64(-1)
	}
	return xdr.ScVal{
		Type: xdr.ScValTypeScvI128,
		I128: &xdr.Int128Parts{Hi: hi, Lo: lo},
	}
}

// ── XDR helpers ───────────────────────────────────────────────────────────────

func contractAddressToXDR(address string) (xdr.ScAddress, error) {
	raw, err := strkey.Decode(strkey.VersionByteContract, address)
	if err != nil {
		return xdr.ScAddress{}, fmt.Errorf("%w: %s", ErrInvalidContract, address)
	}
	var id xdr.ContractId
	copy(id[:], raw)
	return xdr.ScAddress{
		Type:       xdr.ScAddressTypeScAddressTypeContract,
		ContractId: &id,
	}, nil
}

func accountAddressToXDR(address string) (xdr.ScAddress, error) {
	raw, err := strkey.Decode(strkey.VersionByteAccountID, address)
	if err != nil {
		return xdr.ScAddress{}, fmt.Errorf("invalid account address: %s", address)
	}
	var key xdr.Uint256
	copy(key[:], raw)
	accountID := xdr.AccountId(xdr.PublicKey{
		Type:    xdr.PublicKeyTypePublicKeyTypeEd25519,
		Ed25519: &key,
	})
	return xdr.ScAddress{
		Type:      xdr.ScAddressTypeScAddressTypeAccount,
		AccountId: &accountID,
	}, nil
}

// hostFunctionName extracts the invoked contract function name from a host
// function, for the signer intent. It returns an empty string for host function
// types that do not name a function, which the signer treats as an unknown
// operation and refuses.
func hostFunctionName(fn xdr.HostFunction) string {
	if fn.Type != xdr.HostFunctionTypeHostFunctionTypeInvokeContract || fn.InvokeContract == nil {
		return ""
	}
	return string(fn.InvokeContract.FunctionName)
}

// hostFunctionContract extracts the target contract address from a host
// function, for the signer intent. It returns an empty string when the host
// function does not carry a contract address, which the signer refuses.
func hostFunctionContract(fn xdr.HostFunction) string {
	if fn.Type != xdr.HostFunctionTypeHostFunctionTypeInvokeContract || fn.InvokeContract == nil {
		return ""
	}
	addr, err := fn.InvokeContract.ContractAddress.String()
	if err != nil {
		return ""
	}
	return addr
}
