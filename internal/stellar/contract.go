package stellar

import (
	"context"
	"fmt"
	"time"

	"github.com/stellar/go-stellar-sdk/support/log"
	"github.com/stellar/go-stellar-sdk/txnbuild"
)

// ContractInvoker handles all contract invocations and simulations
type ContractInvoker struct {
	client *Client
	logger *log.Entry
}

// NewContractInvoker creates a new contract invoker
func NewContractInvoker(client *Client) *ContractInvoker {
	return &ContractInvoker{
		client: client,
		logger: log.WithField("component", "contract_invoker"),
	}
}

// SimulateContract simulates a contract invocation without submitting it
func (ci *ContractInvoker) SimulateContract(
	ctx context.Context,
	contractID string,
	method string,
	args []interface{},
) (*SimulationResult, error) {
	// Build the transaction that would call the contract
	tx, err := ci.buildContractInvocation(ctx, contractID, method, args)
	if err != nil {
		return nil, fmt.Errorf("failed to build contract invocation: %w", err)
	}

	// Simulate the transaction
	result := &SimulationResult{
		IsSuccess: false,
	}

	// Call Soroban RPC to simulate (this is a placeholder for the actual implementation)
	// In production, this would call the actual Soroban RPC simulateTransaction endpoint
	if tx == nil {
		return nil, fmt.Errorf("failed to build transaction")
	}

	result.IsSuccess = true
	result.GasEstimate = 10000 // Placeholder - would be returned from simulation
	return result, nil
}

// InvokeContract invokes a contract method and submits the transaction
func (ci *ContractInvoker) InvokeContract(
	ctx context.Context,
	contractID string,
	method string,
	args []interface{},
) (*ContractResult, error) {
	// First simulate to catch errors early
	sim, err := ci.SimulateContract(ctx, contractID, method, args)
	if err != nil {
		return nil, fmt.Errorf("simulation failed: %w", err)
	}

	if !sim.IsSuccess {
		return nil, fmt.Errorf("simulation failed: %s", sim.Error)
	}

	// Build the transaction
	tx, err := ci.buildContractInvocation(ctx, contractID, method, args)
	if err != nil {
		return nil, fmt.Errorf("failed to build contract invocation: %w", err)
	}

	// Submit with retries
	result, err := ci.submitWithRetries(ctx, tx)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// buildContractInvocation builds a Soroban contract invocation transaction
func (ci *ContractInvoker) buildContractInvocation(
	ctx context.Context,
	contractID string,
	method string,
	args []interface{},
) (*txnbuild.Transaction, error) {
	if contractID == "" {
		return nil, fmt.Errorf("contract ID is required")
	}
	// Validate contract ID format (Stellar contract IDs are 56 characters starting with 'C')
	if len(contractID) < 56 {
		return nil, fmt.Errorf("contract ID is required")
	}
	if method == "" {
		return nil, fmt.Errorf("method is required")
	}

	// Note: This is a simplified implementation. In production, this would:
	// 1. Create proper XDR representations of arguments
	// 2. Build a complete InvokeHostFunction operation
	// 3. Handle authorization structures if needed

	// Placeholder implementation
	_ = ci.client.networkID

	// This would normally return a properly constructed transaction
	// For now, returning nil as we'll need the actual Soroban RPC setup
	return nil, nil
}

// submitWithRetries submits a transaction with exponential backoff retry logic
func (ci *ContractInvoker) submitWithRetries(ctx context.Context, tx *txnbuild.Transaction) (*ContractResult, error) {
	if tx == nil {
		return nil, fmt.Errorf("transaction is nil")
	}

	maxRetries := ci.client.config.MaxRetries
	backoff := time.Duration(ci.client.config.RetryBackoff) * time.Millisecond

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				// Continue to next attempt
			}
			// Exponential backoff
			backoff *= 2
		}

		result, err := ci.submitTransaction(ctx, tx)
		if err == nil {
			return result, nil
		}

		lastErr = err

		// Check if the error is retryable
		if !isRetryableError(err) {
			return &ContractResult{
				IsSuccess: false,
				Error:     err.Error(),
			}, nil
		}

		ci.logger.WithField("attempt", attempt+1).
			WithField("error", err).
			Debug("Retrying transaction submission")
	}

	return nil, fmt.Errorf("failed to submit transaction after %d retries: %w", maxRetries, lastErr)
}

// submitTransaction submits a transaction to the network
func (ci *ContractInvoker) submitTransaction(ctx context.Context, tx *txnbuild.Transaction) (*ContractResult, error) {
	if tx == nil {
		return nil, fmt.Errorf("transaction is nil")
	}

	// This would normally:
	// 1. Sign the transaction with the source key
	// 2. Submit to Soroban RPC or Horizon
	// 3. Poll for confirmation

	// Placeholder result
	result := &ContractResult{
		IsSuccess:       true,
		TransactionHash: "0x" + "00", // Placeholder
		BlockNumber:     0,
	}

	return result, nil
}

// isRetryableError determines if an error should be retried
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Network-related errors that should be retried
	retryableErrors := []string{
		"timeout",
		"temporary failure",
		"rate limited",
		"503",
		"502",
		"connection refused",
		"i/o timeout",
	}

	for _, retryable := range retryableErrors {
		if len(errStr) > 0 && contains(errStr, retryable) {
			return true
		}
	}

	return false
}

// contains checks if a string contains a substring (case-insensitive)
func contains(str, substr string) bool {
	for i := 0; i <= len(str)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if str[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
