package service

import (
	"context"
	"fmt"

	"github.com/suncrestlabs/nester/apps/api/internal/signing"
	"github.com/suncrestlabs/nester/apps/api/internal/stellar"
)

// SorobanVaultChainInvoker implements VaultChainInvoker by submitting
// InvokeHostFunction transactions to the Soroban RPC node.
type SorobanVaultChainInvoker struct {
	invoker            *stellar.ContractInvoker
	defaultSlippageBps int
}

// NewIsolatedSorobanVaultChainInvoker builds a chain invoker that delegates
// signing to the standalone signer process over a Unix domain socket.
//
// This process holds no operator key: operatorAddress is the operator's public
// address, needed only so transactions are built against the correct source
// account. This is the recommended production configuration; see
// docs/security/signing-isolation.md.
func NewIsolatedSorobanVaultChainInvoker(
	rpcURL, horizonURL, networkPassphrase, operatorAddress, signerSocketPath string,
	defaultSlippageBps int,
) (*SorobanVaultChainInvoker, error) {
	client, err := signing.NewClient(signing.ClientOptions{SocketPath: signerSocketPath})
	if err != nil {
		return nil, fmt.Errorf("build signer client: %w", err)
	}
	remote, err := stellar.NewRemoteSigner(client, operatorAddress, networkPassphrase)
	if err != nil {
		return nil, fmt.Errorf("build remote signer: %w", err)
	}
	inv, err := stellar.NewContractInvokerWithSigner(rpcURL, horizonURL, networkPassphrase, remote)
	if err != nil {
		return nil, err
	}
	return &SorobanVaultChainInvoker{
		invoker:            inv,
		defaultSlippageBps: defaultSlippageBps,
	}, nil
}

func NewSorobanVaultChainInvoker(
	rpcURL, horizonURL, networkPassphrase, operatorSecret string,
	defaultSlippageBps int,
) (*SorobanVaultChainInvoker, error) {
	inv, err := stellar.NewContractInvoker(rpcURL, horizonURL, networkPassphrase, operatorSecret)
	if err != nil {
		return nil, err
	}
	return &SorobanVaultChainInvoker{
		invoker:            inv,
		defaultSlippageBps: defaultSlippageBps,
	}, nil
}

func (s *SorobanVaultChainInvoker) PauseVault(ctx context.Context, contractAddress string) error {
	return s.invoker.InvokeVoidFunction(ctx, contractAddress, "pause")
}

func (s *SorobanVaultChainInvoker) UnpauseVault(ctx context.Context, contractAddress string) error {
	return s.invoker.InvokeVoidFunction(ctx, contractAddress, "unpause")
}

func (s *SorobanVaultChainInvoker) RebalanceVault(ctx context.Context, contractAddress string) (string, error) {
	return s.invoker.InvokeVoidFunctionSubmit(ctx, contractAddress, "rebalance")
}

func (s *SorobanVaultChainInvoker) SimulateRebalanceVault(ctx context.Context, contractAddress string) error {
	return s.invoker.SimulateVoidFunction(ctx, contractAddress, "rebalance")
}

func (s *SorobanVaultChainInvoker) SetAllocationWeights(
	ctx context.Context,
	strategyContractAddress string,
	weights []AllocationWeightEntry,
) error {
	stellarWeights := make([]stellar.AllocationWeightEntry, len(weights))
	for i, w := range weights {
		stellarWeights[i] = stellar.AllocationWeightEntry{
			Protocol:  w.Protocol,
			WeightBps: w.WeightBps,
		}
	}
	return s.invoker.InvokeSetWeights(ctx, strategyContractAddress, stellarWeights)
}

// DepositToVault invokes the vault contract's deposit function with the
// operator as both caller and depositing user, passing amount and zero
// as the minimum-shares-out slippage guard.
func (s *SorobanVaultChainInvoker) DepositToVault(ctx context.Context, contractAddress string, amountStroops int64) error {
	_, err := s.invoker.InvokeWithI128Pair(ctx, contractAddress, "deposit", amountStroops, 0)
	return err
}

// WithdrawFromVault invokes the vault contract's withdraw function with a
// slippage-safe min_assets_out derived from withdrawal_fee_preview.
func (s *SorobanVaultChainInvoker) WithdrawFromVault(
	ctx context.Context,
	contractAddress string,
	sharesStroops int64,
	slippageBps int,
) (string, error) {
	bps, err := stellar.ResolveSlippageBps(slippageBps, s.defaultSlippageBps)
	if err != nil {
		return "", fmt.Errorf("invalid slippage: %w", err)
	}

	previewNet, err := s.invoker.PreviewWithdrawNet(ctx, contractAddress, sharesStroops)
	if err != nil {
		return "", fmt.Errorf("preview withdrawal: %w", err)
	}

	minAssetsOut := stellar.ComputeMinAssetsOut(previewNet, bps)
	return s.invoker.InvokeWithI128Pair(ctx, contractAddress, "withdraw", sharesStroops, minAssetsOut)
}

// HarvestVault invokes vault.harvest(user, compound) for the given Stellar account.
func (s *SorobanVaultChainInvoker) HarvestVault(
	ctx context.Context,
	contractAddress, userAddress string,
	compound bool,
) (string, error) {
	return s.invoker.InvokeWithAddressAndBool(ctx, contractAddress, "harvest", userAddress, compound)
}

// PreviewWithdrawNet calls preview_withdraw_net on the vault contract and
// returns the POST-FEE net amount (in stroops) the user actually receives after
// all vault fees have been deducted. This is the correct value to use as
// min_assets_out when building a withdraw transaction — WithdrawFromVault
// already uses this method for its slippage guard.
func (s *SorobanVaultChainInvoker) PreviewWithdrawNet(ctx context.Context, contractAddress string, sharesStroops int64) (int64, error) {
	val, err := s.invoker.QueryWithI128Arg(ctx, contractAddress, "preview_withdraw_net", sharesStroops)
	if err != nil {
		return 0, err
	}
	// Bounds-checked rather than a direct int64() conversion: a value above
	// int64 max would otherwise truncate into a negative amount.
	return stellar.I128ScValToInt64(val)
}

func (s *SorobanVaultChainInvoker) PreviewDeposit(ctx context.Context, contractAddress string, amountStroops int64) (int64, error) {
	val, err := s.invoker.QueryWithI128Arg(ctx, contractAddress, "preview_deposit", amountStroops)
	if err != nil {
		return 0, err
	}
	// Bounds-checked rather than a direct int64() conversion: a value above
	// int64 max would otherwise truncate into a negative amount.
	return stellar.I128ScValToInt64(val)
}

// PreviewWithdraw calls preview_withdraw on the vault contract and returns the
// GROSS PRE-FEE amount (in stroops) — the raw share-to-asset conversion before
// any vault fees are applied. This value is higher than what the user will
// actually receive. Callers that need the net (post-fee) amount MUST use
// PreviewWithdrawNet instead. The slippage guard in WithdrawFromVault already
// uses PreviewWithdrawNet to avoid over-estimating the minimum assets out.
func (s *SorobanVaultChainInvoker) PreviewWithdraw(ctx context.Context, contractAddress string, sharesStroops int64) (int64, error) {
	val, err := s.invoker.QueryWithI128Arg(ctx, contractAddress, "preview_withdraw", sharesStroops)
	if err != nil {
		return 0, err
	}
	// Bounds-checked rather than a direct int64() conversion: a value above
	// int64 max would otherwise truncate into a negative amount.
	return stellar.I128ScValToInt64(val)
}

// EmergencyWithdrawAll invokes the vault contract's emergency_withdraw_all
// function with the operator as the authorizing user, exiting every active
// position in a single transaction.
func (s *SorobanVaultChainInvoker) EmergencyWithdrawAll(ctx context.Context, contractAddress string) error {
	return s.invoker.InvokeVoidFunction(ctx, contractAddress, "emergency_withdraw_all")
}
