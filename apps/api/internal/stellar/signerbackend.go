package stellar

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/txnbuild"
	"github.com/stellar/go/xdr"

	"github.com/suncrestlabs/nester/apps/api/internal/signing"
)

// SigningBackend builds and signs transactions inside the signer process.
//
// It is the only component in the system that holds the operator secret. It
// implements signing.Backend, so the policy, kill-switch, replay and audit
// layers in internal/signing sit in front of every call to it.
//
// The critical property: BuildAndSign constructs the transaction from the
// validated intent using this process's own RPC connection. It never signs
// caller-supplied bytes. A caller that manages to reach the signer can
// therefore only obtain signatures over transactions this code built from an
// intent the policy approved.
type SigningBackend struct {
	kp                *keypair.Full
	networkPassphrase string
	invoker           *ContractInvoker
}

// NewSigningBackend builds the signer-side backend.
//
// It constructs its own ContractInvoker for simulation and sequence lookups,
// wired to a LocalSigner holding the same key. That is correct here and only
// here: this IS the process trusted with the key.
func NewSigningBackend(rpcURL, horizonURL, networkPassphrase, operatorSecret string) (*SigningBackend, error) {
	kp, err := keypair.ParseFull(operatorSecret)
	if err != nil {
		// Not wrapped: keypair errors can echo the malformed secret.
		return nil, errors.New("invalid operator secret")
	}
	local := &LocalSigner{kp: kp, networkPassphrase: networkPassphrase}
	invoker, err := NewContractInvokerWithSigner(rpcURL, horizonURL, networkPassphrase, local)
	if err != nil {
		return nil, fmt.Errorf("build signer invoker: %w", err)
	}
	return &SigningBackend{
		kp:                kp,
		networkPassphrase: networkPassphrase,
		invoker:           invoker,
	}, nil
}

// KeyID returns the operator public address.
//
// The public address is the correct key identifier for audit records: it names
// which key signed without revealing anything secret, and an investigator can
// correlate it directly with on-chain activity.
func (b *SigningBackend) KeyID() string {
	return b.kp.Address()
}

// BuildAndSign constructs the transaction described by the intent and signs it.
//
// The intent has already passed structural validation, deployment policy, the
// replay guard, and the kill switch by the time this runs. What remains is
// translating the approved intent into the exact host function and signing it.
func (b *SigningBackend) BuildAndSign(ctx context.Context, i *signing.Intent) (string, error) {
	// Defence in depth: the network was checked by the policy, but the key
	// lives here and applying it under the wrong network passphrase produces a
	// signature valid on a chain the operator did not intend. Re-checking
	// costs a string comparison.
	if i.NetworkPassphrase != b.networkPassphrase {
		return "", errors.New("intent network does not match the signer network")
	}

	hostFn, err := b.hostFunctionFor(i)
	if err != nil {
		return "", err
	}
	return b.invoker.buildSimulateSign(ctx, hostFn, i)
}

// hostFunctionFor translates a validated intent into the Soroban host function
// it describes.
//
// The switch is exhaustive over the shapes the signer accepts. An intent whose
// shape is not handled here is refused rather than falling through to a
// permissive default — a default case that built *something* would be a way to
// reach the key with an unmodelled transaction.
func (b *SigningBackend) hostFunctionFor(i *signing.Intent) (xdr.HostFunction, error) {
	contractScAddr, err := contractAddressToXDR(i.ContractAddress)
	if err != nil {
		return xdr.HostFunction{}, fmt.Errorf("intent contract address: %w", err)
	}
	callerScAddr, err := accountAddressToXDR(b.kp.Address())
	if err != nil {
		return xdr.HostFunction{}, fmt.Errorf("operator address: %w", err)
	}

	var args []xdr.ScVal
	switch i.Shape {
	case signing.ShapeVoid:
		args = []xdr.ScVal{
			{Type: xdr.ScValTypeScvAddress, Address: &callerScAddr},
		}
	case signing.ShapeI128Pair:
		args = []xdr.ScVal{
			{Type: xdr.ScValTypeScvAddress, Address: &callerScAddr},
			int64ToI128ScVal(i.Arg0),
			int64ToI128ScVal(i.Arg1),
		}
	case signing.ShapeAddressBool:
		userScAddr, aerr := accountAddressToXDR(i.Address)
		if aerr != nil {
			return xdr.HostFunction{}, fmt.Errorf("intent address argument: %w", aerr)
		}
		flag := i.Flag
		args = []xdr.ScVal{
			{Type: xdr.ScValTypeScvAddress, Address: &userScAddr},
			{Type: xdr.ScValTypeScvBool, B: &flag},
		}
	case signing.ShapeWeights:
		vec, werr := weightsToScVal(i.Weights)
		if werr != nil {
			return xdr.HostFunction{}, werr
		}
		args = []xdr.ScVal{
			{Type: xdr.ScValTypeScvAddress, Address: &callerScAddr},
			vec,
		}
	default:
		return xdr.HostFunction{}, fmt.Errorf("signer cannot build shape %q", i.Shape)
	}

	return xdr.HostFunction{
		Type: xdr.HostFunctionTypeHostFunctionTypeInvokeContract,
		InvokeContract: &xdr.InvokeContractArgs{
			ContractAddress: contractScAddr,
			FunctionName:    xdr.ScSymbol(string(i.Operation)),
			Args:            args,
		},
	}, nil
}

// weightsToScVal encodes an allocation vector exactly as the strategy contract
// expects it: a vector of maps keyed by source_id and weight_bps.
//
// The encoding mirrors InvokeSetWeights in invoker.go. It is duplicated rather
// than shared because the two build from different input types, and a signer
// that encoded weights differently from the API would sign a transaction that
// did not match what the caller asked for.
func weightsToScVal(weights []signing.WeightEntry) (xdr.ScVal, error) {
	if len(weights) == 0 {
		return xdr.ScVal{}, errors.New("weight vector must not be empty")
	}
	items := make([]xdr.ScVal, 0, len(weights))
	for _, w := range weights {
		if w.WeightBps < 0 {
			return xdr.ScVal{}, errors.New("weight must not be negative")
		}
		// The policy bounds WeightBps to 0..10000 before this runs, and the
		// negative case is re-checked immediately above, so the conversion is
		// exact.
		bps := xdr.Uint32(w.WeightBps) // #nosec G115 -- bounded to 0..10000 by policy and re-checked above
		sourceSym := xdr.ScSymbol(strings.TrimSpace(w.Protocol))
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
		items = append(items, xdr.ScVal{Type: xdr.ScValTypeScvMap, Map: &mapPtr})
	}
	scVec := xdr.ScVec(items)
	vecPtr := &scVec
	return xdr.ScVal{Type: xdr.ScValTypeScvVec, Vec: &vecPtr}, nil
}

// buildSimulateSign performs the build/simulate/fee-patch/sign sequence for a
// host function assembled by the signer.
//
// It mirrors submitHostFunction but stops at signing: submission stays with the
// API, so the signer needs no ability to broadcast. Keeping the signer off the
// submission path means a compromised signer cannot quietly push transactions
// without the API observing them.
func (c *ContractInvoker) buildSimulateSign(ctx context.Context, hostFn xdr.HostFunction, intent *signing.Intent) (string, error) {
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
			&txnbuild.InvokeHostFunction{HostFunction: hostFn},
		},
		BaseFee:       txnbuild.MinBaseFee,
		Preconditions: txnbuild.Preconditions{TimeBounds: txnbuild.NewTimeout(int64(signerTxTimeoutSeconds))},
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

	patched, err := applySorobanData(tx, simResult)
	if err != nil {
		return "", err
	}

	return c.signEnvelope(ctx, SignRequest{
		EnvelopeXDR:     patched,
		Operation:       string(intent.Operation),
		ContractAddress: intent.ContractAddress,
		Arg0:            intent.Arg0,
		Arg1:            intent.Arg1,
		Address:         intent.Address,
		Flag:            intent.Flag,
		RequestID:       intent.RequestID,
	})
}

// signerTxTimeoutSeconds bounds how long a signed transaction stays valid on
// the network. It matches the timeout the invoker already uses for its own
// builds, so the signer does not widen the window during which a signed
// transaction can be submitted.
const signerTxTimeoutSeconds = 300

// applySorobanData patches simulation results into the built transaction and
// returns the fee-adjusted envelope, ready for signing.
func applySorobanData(tx *txnbuild.Transaction, simResult simulateResult) (string, error) {
	var sorobanData xdr.SorobanTransactionData
	if err := xdr.SafeUnmarshalBase64(simResult.TransactionData, &sorobanData); err != nil {
		return "", fmt.Errorf("decode soroban data: %w", err)
	}

	envelope := tx.ToXDR()
	envelope.V1.Tx.Ext = xdr.TransactionExt{V: 1, SorobanData: &sorobanData}

	minFee, err := parseMinResourceFee(simResult.MinResourceFee)
	if err != nil {
		return "", err
	}
	// Guard the fee before narrowing to the XDR uint32. An unexpectedly large
	// simulated resource fee should be refused rather than silently truncated
	// into a small one — truncation would produce a transaction that fails on
	// submission, but the check also bounds what the operator can be made to
	// spend by a manipulated simulation response.
	total := int64(txnbuild.MinBaseFee) + minFee
	if total <= 0 || total > maxTransactionFeeStroops {
		return "", fmt.Errorf("simulated transaction fee %d stroops is outside the permitted range", total)
	}
	envelope.V1.Tx.Fee = xdr.Uint32(total) // #nosec G115 -- bounded above by maxTransactionFeeStroops

	envB64, err := xdr.MarshalBase64(envelope)
	if err != nil {
		return "", fmt.Errorf("encode patched envelope: %w", err)
	}
	return envB64, nil
}

// maxTransactionFeeStroops caps the total fee the signer will sign for.
//
// 10 XLM. Soroban resource fees for the vault operations sit far below this;
// the bound exists so that a compromised or malfunctioning RPC node cannot
// induce the operator to sign a transaction that drains its balance in fees.
const maxTransactionFeeStroops = 100_000_000

func parseMinResourceFee(raw string) (int64, error) {
	var v int64
	if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%d", &v); err != nil {
		return 0, fmt.Errorf("parse simulation min resource fee %q: %w", raw, err)
	}
	if v < 0 {
		return 0, fmt.Errorf("simulation returned a negative resource fee")
	}
	return v, nil
}
