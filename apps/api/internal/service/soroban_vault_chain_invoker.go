package service

import (
	"context"

	"github.com/suncrestlabs/nester/apps/api/internal/stellar"
)

// SorobanVaultChainInvoker implements VaultChainInvoker by submitting
// InvokeHostFunction transactions to the Soroban RPC node.
type SorobanVaultChainInvoker struct {
	invoker *stellar.ContractInvoker
}

func NewSorobanVaultChainInvoker(
	rpcURL, horizonURL, networkPassphrase, operatorSecret string,
) (*SorobanVaultChainInvoker, error) {
	inv, err := stellar.NewContractInvoker(rpcURL, horizonURL, networkPassphrase, operatorSecret)
	if err != nil {
		return nil, err
	}
	return &SorobanVaultChainInvoker{invoker: inv}, nil
}

func (s *SorobanVaultChainInvoker) PauseVault(ctx context.Context, contractAddress string) error {
	return s.invoker.InvokeVoidFunction(ctx, contractAddress, "pause")
}

func (s *SorobanVaultChainInvoker) UnpauseVault(ctx context.Context, contractAddress string) error {
	return s.invoker.InvokeVoidFunction(ctx, contractAddress, "unpause")
}

// DepositToVault invokes the vault contract's deposit function with the
// operator as both caller and depositing user, passing amount and zero
// as the minimum-shares-out slippage guard.
func (s *SorobanVaultChainInvoker) DepositToVault(ctx context.Context, contractAddress string, amountStroops int64) error {
	return s.invoker.InvokeWithI128Pair(ctx, contractAddress, "deposit", amountStroops, 0)
}

// WithdrawFromVault invokes the vault contract's withdraw function with the
// operator as both caller and withdrawing user, passing shares and zero
// as the minimum-assets-out slippage guard.
func (s *SorobanVaultChainInvoker) WithdrawFromVault(ctx context.Context, contractAddress string, sharesStroops int64) error {
	return s.invoker.InvokeWithI128Pair(ctx, contractAddress, "withdraw", sharesStroops, 0)
}
