package stellar

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/signing"
)

// RemoteSigner delegates signing to the standalone signer process.
//
// This is the isolated custody model. The API process holds no operator key:
// it builds and simulates transactions to learn their resource requirements,
// then describes what it wants signed as a typed intent. The signer rebuilds
// and re-validates that intent before applying the key.
//
// Note what is deliberately discarded: SignRequest.EnvelopeXDR, the envelope
// the API built. The signer does not sign it. Sending an envelope for signature
// is exactly the oracle pattern this design rejects, so the API's envelope is
// used only for its own simulation and fee accounting, and the signer builds
// its own from the intent.
type RemoteSigner struct {
	client          *signing.Client
	operatorAddress string
	network         string
}

// NewRemoteSigner builds a signer that calls the remote signing service.
//
// operatorAddress is the public Stellar address the signer signs as. The API
// needs it to build transactions against the correct source account; it is
// public data and holding it grants no signing capability.
func NewRemoteSigner(client *signing.Client, operatorAddress, networkPassphrase string) (*RemoteSigner, error) {
	if client == nil {
		return nil, errors.New("remote signer requires a client")
	}
	operatorAddress = strings.TrimSpace(operatorAddress)
	if operatorAddress == "" {
		return nil, errors.New("remote signer requires the operator public address")
	}
	if !strings.HasPrefix(operatorAddress, "G") {
		return nil, errors.New("operator address must be a G-prefixed public address, not a secret seed")
	}
	return &RemoteSigner{
		client:          client,
		operatorAddress: operatorAddress,
		network:         networkPassphrase,
	}, nil
}

// OperatorAddress returns the public address transactions are built against.
func (s *RemoteSigner) OperatorAddress() string {
	return s.operatorAddress
}

// SignEnvelope translates the request into a signing intent and asks the remote
// signer to sign it.
func (s *RemoteSigner) SignEnvelope(ctx context.Context, req SignRequest) (string, error) {
	intent, err := s.intentFor(req)
	if err != nil {
		return "", err
	}
	result, err := s.client.Sign(ctx, intent)
	if err != nil {
		return "", err
	}
	return result.SignedXDR, nil
}

// intentFor builds the typed intent describing what the caller wants signed.
//
// The shape is derived from the operation rather than taken from the caller, so
// a mismatch between the two cannot originate here; the signer re-derives it
// independently and rejects any disagreement.
func (s *RemoteSigner) intentFor(req SignRequest) (*signing.Intent, error) {
	op := signing.Operation(strings.TrimSpace(req.Operation))
	shape, known := signing.ShapeFor(op)
	if !known {
		return nil, fmt.Errorf("%w: operation %q is not signable", signing.ErrPolicyRejected, op)
	}

	intent := &signing.Intent{
		ID:                uuid.NewString(),
		Operation:         op,
		Shape:             shape,
		ContractAddress:   strings.TrimSpace(req.ContractAddress),
		NetworkPassphrase: s.network,
		IssuedAt:          time.Now().UTC(),
		RequestID:         req.RequestID,
	}

	// Populate only the fields the shape uses. Copying every field regardless
	// would let a stray value on an unrelated field trip the signer's
	// shape check and turn a benign caller bug into a signing outage.
	switch shape {
	case signing.ShapeVoid:
		// No arguments.
	case signing.ShapeI128Pair:
		intent.Arg0 = req.Arg0
		intent.Arg1 = req.Arg1
	case signing.ShapeAddressBool:
		intent.Address = strings.TrimSpace(req.Address)
		intent.Flag = req.Flag
	case signing.ShapeWeights:
		intent.Weights = make([]signing.WeightEntry, 0, len(req.Weights))
		for _, w := range req.Weights {
			if w.WeightBps > maxWeightBps {
				return nil, fmt.Errorf("%w: weight %d exceeds %d bps",
					signing.ErrPolicyRejected, w.WeightBps, maxWeightBps)
			}
			// WeightBps is uint32 on the invoker side and int32 in the intent.
			// The bound above keeps the conversion exact.
			intent.Weights = append(intent.Weights, signing.WeightEntry{
				Protocol:  w.Protocol,
				WeightBps: int32(w.WeightBps), // #nosec G115 -- bounded to maxWeightBps immediately above
			})
		}
	}

	return intent, nil
}

// maxWeightBps is the largest weight a single allocation entry may carry.
const maxWeightBps = 10_000
