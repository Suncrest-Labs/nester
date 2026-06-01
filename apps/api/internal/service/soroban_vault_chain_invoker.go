package service

import (
	"context"
	"fmt"

	"github.com/suncrestlabs/nester/apps/api/internal/stellar"
)

// SorobanVaultChainInvoker implements VaultChainInvoker by submitting
// InvokeHostFunction transactions to the Soroban RPC node.
type SorobanVaultChainInvoker struct {
	invoker              *stellar.ContractInvoker
	defaultSlippageBps   int
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
	return s.invoker.InvokeWithI128Pair(ctx, contractAddress, "deposit", amountStroops, 0)
}

// WithdrawFromVault invokes the vault contract's withdraw function with a
// slippage-safe min_assets_out derived from withdrawal_fee_preview.
func (s *SorobanVaultChainInvoker) WithdrawFromVault(
	ctx context.Context,
	contractAddress string,
	sharesStroops int64,
	slippageBps int,
) error {
	bps, err := stellar.ResolveSlippageBps(slippageBps, s.defaultSlippageBps)
	if err != nil {
		return fmt.Errorf("invalid slippage: %w", err)
	}

	previewNet, err := s.invoker.PreviewWithdrawNet(ctx, contractAddress, sharesStroops)
	if err != nil {
		return fmt.Errorf("preview withdrawal: %w", err)
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
