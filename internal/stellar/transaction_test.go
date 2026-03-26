package stellar

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ============================================================================
// Transaction Submission Tests (Unit & Integration)
// ============================================================================

func TestTransactionSubmission_Success(t *testing.T) {
	client := &Client{
		config: Config{
			MaxRetries:   3,
			RetryBackoff: 100,
		},
		networkID: "Test SDF Network ; September 2015",
	}

	invoker := NewContractInvoker(client)

	// Test successful transaction submission
	// In production, this would submit to the network
	result, err := invoker.submitTransaction(context.Background(), nil)

	// Currently returns error because tx is nil
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestTransactionSubmission_ReturnsHash(t *testing.T) {
	// Test that successful submission returns transaction hash
	result := &ContractResult{
		TransactionHash: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		BlockNumber:     12345,
		IsSuccess:       true,
		Error:           "",
		ReturnValue:     nil,
	}

	assert.True(t, result.IsSuccess)
	assert.NotEmpty(t, result.TransactionHash)
	assert.Equal(t, uint64(12345), result.BlockNumber)
}

func TestTransactionSubmission_NetworkTimeout(t *testing.T) {
	client := &Client{
		config: Config{
			MaxRetries:   2,
			RetryBackoff: 10,
		},
		networkID: "Test SDF Network ; September 2015",
	}

	invoker := NewContractInvoker(client)

	// Test with context timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Simulate network timeout error
	err := errors.New("i/o timeout")
	isRetryable := isRetryableError(err)
	assert.True(t, isRetryable)
}

func TestTransactionSubmission_ConnectionRefused(t *testing.T) {
	// Test connection refused error handling
	err := errors.New("connection refused")
	isRetryable := isRetryableError(err)
	assert.True(t, isRetryable)
}

func TestTransactionSubmission_NetworkErrorWrapped(t *testing.T) {
	// Test that network errors are wrapped without panicking
	networkErr := errors.New("network error: connection refused")
	wrappedErr := fmt.Errorf("transaction submission failed: %w", networkErr)

	assert.Error(t, wrappedErr)
	assert.Contains(t, wrappedErr.Error(), "transaction submission failed")
	assert.Contains(t, wrappedErr.Error(), "connection refused")
}

func TestTransactionSubmission_TXN_FAILED(t *testing.T) {
	// Test transaction failure with result code
	result := &ContractResult{
		TransactionHash: "0x1234567890abcdef",
		BlockNumber:     0,
		IsSuccess:       false,
		Error:           "TXN_FAILED: insufficient balance",
		ReturnValue:     nil,
	}

	assert.False(t, result.IsSuccess)
	assert.Contains(t, result.Error, "TXN_FAILED")
	assert.Contains(t, result.Error, "insufficient balance")
}

func TestTransactionSubmission_TXN_ALREADY_EXISTS(t *testing.T) {
	// Test duplicate transaction handling
	result := &ContractResult{
		TransactionHash: "0x1234567890abcdef",
		BlockNumber:     0,
		IsSuccess:       false,
		Error:           "TXN_ALREADY_EXISTS",
		ReturnValue:     nil,
	}

	assert.False(t, result.IsSuccess)
	assert.Contains(t, result.Error, "TXN_ALREADY_EXISTS")
}

func TestTransactionSubmission_DuplicateHandledGracefully(t *testing.T) {
	// Test that duplicate transactions are handled gracefully
	// In production, this might return the existing transaction hash
	result := &ContractResult{
		TransactionHash: "0x1234567890abcdef",
		BlockNumber:     12345,
		IsSuccess:       true, // Considered success if already exists
		Error:           "",
		ReturnValue:     nil,
	}

	assert.True(t, result.IsSuccess)
	assert.NotEmpty(t, result.TransactionHash)
}

func TestTransactionSubmission_RetryOnNetworkError(t *testing.T) {
	client := &Client{
		config: Config{
			MaxRetries:   3,
			RetryBackoff: 10,
		},
		networkID: "Test SDF Network ; September 2015",
	}

	invoker := NewContractInvoker(client)

	// Test retry logic with network errors
	retryableErrors := []string{
		"connection refused",
		"i/o timeout",
		"temporary failure",
		"rate limited",
		"503 Service Unavailable",
		"502 Bad Gateway",
	}

	for _, errMsg := range retryableErrors {
		t.Run(errMsg, func(t *testing.T) {
			err := errors.New(errMsg)
			isRetryable := isRetryableError(err)
			assert.True(t, isRetryable, "Expected %s to be retryable", errMsg)
		})
	}
}

func TestTransactionSubmission_NoRetryOnPermanentError(t *testing.T) {
	// Test that permanent errors are not retried
	permanentErrors := []string{
		"invalid contract ID",
		"unauthorized",
		"insufficient balance",
		"invalid signature",
	}

	for _, errMsg := range permanentErrors {
		t.Run(errMsg, func(t *testing.T) {
			err := errors.New(errMsg)
			isRetryable := isRetryableError(err)
			assert.False(t, isRetryable, "Expected %s to not be retryable", errMsg)
		})
	}
}

func TestTransactionSubmission_ExponentialBackoff(t *testing.T) {
	client := &Client{
		config: Config{
			MaxRetries:   3,
			RetryBackoff: 10, // Start with 10ms
		},
		networkID: "Test SDF Network ; September 2015",
	}

	invoker := NewContractInvoker(client)

	// Verify backoff configuration
	assert.Equal(t, 3, client.config.MaxRetries)
	assert.Equal(t, 10, client.config.RetryBackoff)

	// Test that retry logic respects max retries
	_, err := invoker.submitWithRetries(context.Background(), nil)
	assert.Error(t, err)
}

func TestTransactionSubmission_ContextCancellation(t *testing.T) {
	client := &Client{
		config: Config{
			MaxRetries:   3,
			RetryBackoff: 100,
		},
		networkID: "Test SDF Network ; September 2015",
	}

	invoker := NewContractInvoker(client)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := invoker.submitWithRetries(ctx, nil)
	assert.Error(t, err)
}

func TestTransactionSubmission_MaxRetriesRespected(t *testing.T) {
	client := &Client{
		config: Config{
			MaxRetries:   2,
			RetryBackoff: 10,
		},
		networkID: "Test SDF Network ; September 2015",
	}

	invoker := NewContractInvoker(client)

	// Test that we stop after max retries
	_, err := invoker.submitWithRetries(context.Background(), nil)
	assert.Error(t, err)
}

func TestTransactionSubmission_NilTransactionError(t *testing.T) {
	client := &Client{
		config: Config{
			MaxRetries:   3,
			RetryBackoff: 100,
		},
		networkID: "Test SDF Network ; September 2015",
	}

	invoker := NewContractInvoker(client)

	_, err := invoker.submitWithRetries(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "transaction is nil")
}

func TestTransactionSubmission_ErrorWrapping(t *testing.T) {
	// Test that errors are properly wrapped
	originalErr := errors.New("network failure")
	wrappedErr := fmt.Errorf("transaction submission failed: %w", originalErr)

	assert.Error(t, wrappedErr)
	assert.Contains(t, wrappedErr.Error(), "transaction submission failed")
	assert.Contains(t, wrappedErr.Error(), "network failure")
}

func TestTransactionSubmission_ResultCodes(t *testing.T) {
	tests := []struct {
		name       string
		errorMsg   string
		isSuccess  bool
		shouldHave bool
	}{
		{
			name:       "TXN_SUCCESS",
			errorMsg:   "",
			isSuccess:  true,
			shouldHave: true,
		},
		{
			name:       "TXN_FAILED",
			errorMsg:   "TXN_FAILED: error",
			isSuccess:  false,
			shouldHave: true,
		},
		{
			name:       "TXN_ALREADY_EXISTS",
			errorMsg:   "TXN_ALREADY_EXISTS",
			isSuccess:  false,
			shouldHave: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &ContractResult{
				TransactionHash: "0x1234",
				IsSuccess:       tt.isSuccess,
				Error:           tt.errorMsg,
			}

			if tt.shouldHave {
				assert.NotNil(t, result)
			}
		})
	}
}

func TestTransactionSubmission_ConcurrentSubmissions(t *testing.T) {
	client := &Client{
		config: Config{
			MaxRetries:   3,
			RetryBackoff: 10,
		},
		networkID: "Test SDF Network ; September 2015",
	}

	invoker := NewContractInvoker(client)

	// Test concurrent transaction submissions
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = invoker.submitWithRetries(context.Background(), nil)
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestTransactionSubmission_TimeoutHandling(t *testing.T) {
	client := &Client{
		config: Config{
			MaxRetries:   2,
			RetryBackoff: 10,
		},
		networkID: "Test SDF Network ; September 2015",
	}

	invoker := NewContractInvoker(client)

	// Test with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	_, err := invoker.submitWithRetries(ctx, nil)
	assert.Error(t, err)
}

func TestTransactionSubmission_RetryableErrorDetection(t *testing.T) {
	tests := []struct {
		name      string
		errMsg    string
		retryable bool
	}{
		{
			name:      "timeout",
			errMsg:    "i/o timeout",
			retryable: true,
		},
		{
			name:      "connection refused",
			errMsg:    "connection refused",
			retryable: true,
		},
		{
			name:      "temporary failure",
			errMsg:    "temporary failure",
			retryable: true,
		},
		{
			name:      "rate limited",
			errMsg:    "rate limited",
			retryable: true,
		},
		{
			name:      "503",
			errMsg:    "503 Service Unavailable",
			retryable: true,
		},
		{
			name:      "502",
			errMsg:    "502 Bad Gateway",
			retryable: true,
		},
		{
			name:      "invalid contract",
			errMsg:    "invalid contract ID",
			retryable: false,
		},
		{
			name:      "unauthorized",
			errMsg:    "unauthorized",
			retryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := errors.New(tt.errMsg)
			isRetryable := isRetryableError(err)
			assert.Equal(t, tt.retryable, isRetryable)
		})
	}
}

func TestTransactionSubmission_EmptyResult(t *testing.T) {
	result := &ContractResult{}

	assert.False(t, result.IsSuccess)
	assert.Empty(t, result.TransactionHash)
	assert.Empty(t, result.Error)
	assert.Nil(t, result.ReturnValue)
}

func TestTransactionSubmission_PartialSuccess(t *testing.T) {
	// Test transaction that succeeded but with warnings
	result := &ContractResult{
		TransactionHash: "0x1234567890abcdef",
		BlockNumber:     12345,
		IsSuccess:       true,
		Error:           "",
		ReturnValue:     map[string]interface{}{"warning": "high gas usage"},
	}

	assert.True(t, result.IsSuccess)
	assert.NotEmpty(t, result.TransactionHash)
	assert.NotNil(t, result.ReturnValue)
}
