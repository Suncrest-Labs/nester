package service

import (
	"context"

	"github.com/suncrestlabs/nester/apps/api/internal/stellar"
)

// rpcChainEventAdapter maps stellar.RPCChainEventVerifier onto
// ChainEventVerifier. The two packages keep distinct result types so stellar
// does not import service (which already depends on stellar).
type rpcChainEventAdapter struct {
	inner *stellar.RPCChainEventVerifier
}

// NewStellarChainEventVerifier builds the production on-chain event verifier
// used by VaultService to record withdrawals from a confirmed tx hash.
func NewStellarChainEventVerifier(rpcURL string) ChainEventVerifier {
	return rpcChainEventAdapter{inner: stellar.NewRPCChainEventVerifier(rpcURL)}
}

func (a rpcChainEventAdapter) VerifyVaultEvent(ctx context.Context, txHash, contractID, eventType string) (VerifiedVaultEvent, error) {
	ev, err := a.inner.VerifyVaultEvent(ctx, txHash, contractID, eventType)
	if err != nil {
		return VerifiedVaultEvent{}, err
	}
	return VerifiedVaultEvent{
		TxHash:     ev.TxHash,
		EventType:  ev.EventType,
		Amount:     ev.Amount,
		ContractID: ev.ContractID,
		Account:    ev.Account,
	}, nil
}
