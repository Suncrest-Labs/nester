package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/stellar/go/xdr"

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
	if val.Type != xdr.ScValTypeScvI128 || val.I128 == nil {
		return 0, errors.New("expected i128 return value")
	}
	return int64(val.I128.Lo), nil
}

func (s *SorobanVaultChainInvoker) PreviewDeposit(ctx context.Context, contractAddress string, amountStroops int64) (int64, error) {
	val, err := s.invoker.QueryWithI128Arg(ctx, contractAddress, "preview_deposit", amountStroops)
	if err != nil {
		return 0, err
	}
	if val.Type != xdr.ScValTypeScvI128 || val.I128 == nil {
		return 0, errors.New("expected i128 return value")
	}
	return int64(val.I128.Lo), nil
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
	if val.Type != xdr.ScValTypeScvI128 || val.I128 == nil {
		return 0, errors.New("expected i128 return value")
	}
	return int64(val.I128.Lo), nil
}
