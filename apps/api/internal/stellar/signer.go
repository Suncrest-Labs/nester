package stellar

import (
	"context"
	"errors"
	"fmt"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/txnbuild"
)

// TransactionSigner applies the operator signature to a fully-built, simulated
// transaction envelope.
//
// Extracting this interface is what removes key custody from the transaction
// building code. ContractInvoker builds and simulates — work that needs no
// secret — and then hands the finished envelope to a signer that may live in
// this process (LocalSigner, for single-process deployments) or behind a
// process boundary (RemoteSigner, talking to the standalone signer).
//
// The interface is deliberately narrow but is NOT a generic byte-signing
// oracle: it accepts a transaction envelope plus the intent that produced it,
// and isolated implementations re-derive and re-validate the transaction from
// that intent before signing. See internal/signing for the policy that
// enforcement runs against.
type TransactionSigner interface {
	// SignEnvelope signs the base64 transaction envelope and returns the
	// signed base64 envelope.
	SignEnvelope(ctx context.Context, req SignRequest) (string, error)
	// OperatorAddress returns the public Stellar address transactions are
	// built against. It is public data — never the secret seed.
	OperatorAddress() string
}

// SignRequest carries everything the signer needs to decide and to sign.
//
// EnvelopeXDR is the built and fee-patched transaction. The remaining fields
// describe what that transaction is supposed to be doing, so an isolated signer
// can verify the envelope matches the declared intent rather than trusting the
// caller description of it.
type SignRequest struct {
	// EnvelopeXDR is the unsigned, simulated, fee-patched transaction envelope.
	EnvelopeXDR string
	// Operation is the contract function being invoked.
	Operation string
	// ContractAddress is the target contract.
	ContractAddress string
	// Arg0 and Arg1 carry i128 arguments where the operation uses them.
	Arg0 int64
	Arg1 int64
	// Address and Flag carry the address/bool argument pair where used.
	Address string
	Flag    bool
	// Weights carries the allocation vector for set_weights.
	Weights []AllocationWeightEntry
	// RequestID correlates the signature with the originating API request.
	RequestID string
}

// ErrNoSigner is returned when a signing operation is attempted on an invoker
// configured without a signer. It is a distinct error so that read-only
// deployments — which legitimately have no operator key — fail with a clear
// message on the signing path while simulation and queries keep working.
var ErrNoSigner = errors.New("no transaction signer is configured")

// LocalSigner holds the operator key in this process.
//
// This is the pre-existing custody model, retained for local development and
// for deployments that have not yet split out the signer process. It is NOT
// the recommended production configuration: with a LocalSigner, code execution
// in the API yields the key. The security posture documented in
// docs/security/signing-isolation.md assumes a RemoteSigner.
//
// Startup logs which mode is active precisely so this distinction is visible in
// production rather than assumed.
type LocalSigner struct {
	kp                *keypair.Full
	networkPassphrase string
}

// NewLocalSigner parses the operator secret and returns an in-process signer.
func NewLocalSigner(operatorSecret, networkPassphrase string) (*LocalSigner, error) {
	kp, err := keypair.ParseFull(operatorSecret)
	if err != nil {
		// The underlying error is not wrapped: keypair parse errors can echo
		// portions of the malformed input, and that input is a secret.
		return nil, errors.New("invalid operator secret")
	}
	return &LocalSigner{kp: kp, networkPassphrase: networkPassphrase}, nil
}

// OperatorAddress returns the operator public address.
func (s *LocalSigner) OperatorAddress() string {
	return s.kp.Address()
}

// SignEnvelope signs the supplied envelope with the in-process key.
func (s *LocalSigner) SignEnvelope(_ context.Context, req SignRequest) (string, error) {
	generic, err := txnbuild.TransactionFromXDR(req.EnvelopeXDR)
	if err != nil {
		return "", fmt.Errorf("parse envelope: %w", err)
	}
	inner, ok := generic.Transaction()
	if !ok {
		return "", errors.New("expected a transaction, got fee-bump")
	}
	signed, err := inner.Sign(s.networkPassphrase, s.kp)
	if err != nil {
		return "", fmt.Errorf("sign transaction: %w", err)
	}
	return signed.Base64()
}
