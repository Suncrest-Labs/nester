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

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/strkey"
	"github.com/stellar/go/txnbuild"
	"github.com/stellar/go/xdr"
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
	kp                *keypair.Full
	httpClient        *http.Client
}

func NewContractInvoker(rpcURL, horizonURL, networkPassphrase, operatorSecret string) (*ContractInvoker, error) {
	kp, err := keypair.ParseFull(operatorSecret)
	if err != nil {
		return nil, fmt.Errorf("invalid operator secret: %w", err)
	}
	return &ContractInvoker{
		rpcURL:            rpcURL,
		horizonURL:        horizonURL,
		networkPassphrase: networkPassphrase,
		kp:                kp,
		httpClient:        &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// InvokeVoidFunction calls a contract function with signature (caller: Address).
func (c *ContractInvoker) InvokeVoidFunction(ctx context.Context, contractAddress, functionName string) error {
	contractScAddr, err := contractAddressToXDR(contractAddress)
	if err != nil {
		return err
	}

	callerScAddr, err := accountAddressToXDR(c.kp.Address())
	if err != nil {
		return err
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
		return fmt.Errorf("get sequence number: %w", err)
	}

	sourceAccount := txnbuild.NewSimpleAccount(c.kp.Address(), seq)

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
		return fmt.Errorf("build transaction: %w", err)
	}

	txB64, err := tx.Base64()
	if err != nil {
		return fmt.Errorf("encode transaction: %w", err)
	}

	simResult, err := c.simulate(ctx, txB64)
	if err != nil {
		return err
	}

	// Decode the SorobanTransactionData returned by simulation.
	var sorobanData xdr.SorobanTransactionData
	if err := xdr.SafeUnmarshalBase64(simResult.TransactionData, &sorobanData); err != nil {
		return fmt.Errorf("decode soroban data: %w", err)
	}

	// Patch the envelope: set soroban data and bump fee.
	envelope := tx.ToXDR()
	envelope.V1.Tx.Ext = xdr.TransactionExt{
		V:           1,
		SorobanData: &sorobanData,
	}
	minFee, _ := strconv.ParseInt(simResult.MinResourceFee, 10, 64)
	envelope.V1.Tx.Fee = xdr.Uint32(txnbuild.MinBaseFee + minFee)

	// Re-parse from the patched XDR so txnbuild can sign it.
	envB64, err := xdr.MarshalBase64(envelope)
	if err != nil {
		return fmt.Errorf("encode patched envelope: %w", err)
	}

	generic, err := txnbuild.TransactionFromXDR(envB64)
	if err != nil {
		return fmt.Errorf("parse patched tx: %w", err)
	}

	inner, ok := generic.Transaction()
	if !ok {
		return errors.New("expected a transaction, got fee-bump")
	}

	signed, err := inner.Sign(c.networkPassphrase, c.kp)
	if err != nil {
		return fmt.Errorf("sign transaction: %w", err)
	}

	signedB64, err := signed.Base64()
	if err != nil {
		return fmt.Errorf("encode signed transaction: %w", err)
	}

	hash, err := c.send(ctx, signedB64)
	if err != nil {
		return err
	}

	return c.waitForTx(ctx, hash)
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
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rpcURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("rpc %s: %w", method, err)
	}
	defer resp.Body.Close()

	return json.NewDecoder(resp.Body).Decode(result)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.horizonURL+"/accounts/"+c.kp.Address(), nil)
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

// InvokeWithI128Pair calls a contract function with signature
// (caller: Address, arg0: i128, arg1: i128). Suitable for deposit and withdraw
// where the operator acts as the transaction source and user.
func (c *ContractInvoker) InvokeWithI128Pair(ctx context.Context, contractAddress, functionName string, arg0, arg1 int64) error {
	contractScAddr, err := contractAddressToXDR(contractAddress)
	if err != nil {
		return err
	}

	callerScAddr, err := accountAddressToXDR(c.kp.Address())
	if err != nil {
		return err
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
		return fmt.Errorf("get sequence number: %w", err)
	}

	sourceAccount := txnbuild.NewSimpleAccount(c.kp.Address(), seq)

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
		return fmt.Errorf("build transaction: %w", err)
	}

	txB64, err := tx.Base64()
	if err != nil {
		return fmt.Errorf("encode transaction: %w", err)
	}

	simResult, err := c.simulate(ctx, txB64)
	if err != nil {
		return err
	}

	var sorobanData xdr.SorobanTransactionData
	if err := xdr.SafeUnmarshalBase64(simResult.TransactionData, &sorobanData); err != nil {
		return fmt.Errorf("decode soroban data: %w", err)
	}

	envelope := tx.ToXDR()
	envelope.V1.Tx.Ext = xdr.TransactionExt{
		V:           1,
		SorobanData: &sorobanData,
	}
	minFee, _ := strconv.ParseInt(simResult.MinResourceFee, 10, 64)
	envelope.V1.Tx.Fee = xdr.Uint32(txnbuild.MinBaseFee + minFee)

	envB64, err := xdr.MarshalBase64(envelope)
	if err != nil {
		return fmt.Errorf("encode patched envelope: %w", err)
	}

	generic, err := txnbuild.TransactionFromXDR(envB64)
	if err != nil {
		return fmt.Errorf("parse patched tx: %w", err)
	}

	inner, ok := generic.Transaction()
	if !ok {
		return errors.New("expected a transaction, got fee-bump")
	}

	signed, err := inner.Sign(c.networkPassphrase, c.kp)
	if err != nil {
		return fmt.Errorf("sign transaction: %w", err)
	}

	signedB64, err := signed.Base64()
	if err != nil {
		return fmt.Errorf("encode signed transaction: %w", err)
	}

	hash, err := c.send(ctx, signedB64)
	if err != nil {
		return err
	}

	return c.waitForTx(ctx, hash)
}

func int64ToI128ScVal(n int64) xdr.ScVal {
	hi := xdr.Int64(0)
	lo := xdr.Uint64(uint64(n))
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
