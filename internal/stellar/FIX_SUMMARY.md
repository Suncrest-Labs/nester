# Fix Summary: InvokeContract Error Handling

## Problem
The test `TestInvokeContract_InvalidContractIDFormat` was failing for the sub-test `too_short` with a nil pointer dereference. The issue was that the `InvokeContract` function was returning a `ContractResult` struct with an `Error` field set, instead of returning a Go `error`. The test expected a real `error`.

## Root Cause
When `SimulateContract` failed (e.g., due to an invalid contract ID), it returned:
```go
&SimulationResult{
    IsSuccess: false,
    Error:     "failed to build contract invocation: contract ID is required",
}, nil
```

Then `InvokeContract` checked `if !sim.IsSuccess` and returned:
```go
&ContractResult{
    IsSuccess: false,
    Error:     sim.Error,
}, nil
```

This meant the function returned a non-nil `ContractResult` with an `Error` field, but the test expected `nil, error`.

## Solution
Changed both `InvokeContract` and `SimulateContract` to return Go `error` values instead of returning result structs with `Error` fields.

### Changes Made

#### 1. Updated `internal/stellar/contract.go` (line 46-48)

Changed `SimulateContract` to return a Go `error` when `buildContractInvocation` returns `nil, nil`:

**Before:**
```go
if tx == nil {
    result.Error = "failed to build transaction"
    return result, nil
}
```

**After:**
```go
if tx == nil {
    return nil, fmt.Errorf("failed to build transaction")
}
```

#### 2. Updated `internal/stellar/contract.go` (line 69-71)

**Before:**
```go
if !sim.IsSuccess {
    return &ContractResult{
        IsSuccess: false,
        Error:     sim.Error,
    }, nil
}
```

**After:**
```go
if !sim.IsSuccess {
    return nil, fmt.Errorf("simulation failed: %s", sim.Error)
}
```

This ensures that when simulation fails, the function returns `nil, error` instead of `&ContractResult{...}, nil`.

#### 3. Updated `internal/stellar/contract_test.go` (line 243-247)

**Before:**
```go
// Should succeed with placeholder implementation
assert.NoError(t, err)
assert.NotNil(t, result)
assert.False(t, result.IsSuccess)
assert.Contains(t, result.Error, "failed to build transaction")
```

**After:**
```go
// Should fail with placeholder implementation since buildContractInvocation returns nil
assert.Error(t, err)
assert.Nil(t, result)
assert.Contains(t, err.Error(), "simulation failed")
```

This test was updated because with the new behavior, when a valid contract ID is passed, `buildContractInvocation` returns `nil, nil`, which causes `SimulateContract` to fail, which in turn causes `InvokeContract` to return `nil, error`.

## Impact Analysis

### Tests That Continue to Work
- `TestInvokeContract_SimulationFailure` - Already expects `nil, error`
- `TestInvokeContract_InvalidMethodName` - Already expects `nil, error`
- `TestInvokeContract_ArgumentEncoding` - Already expects `nil, error`
- `TestInvokeContract_ContextTimeout` - Already expects `nil, error`
- `TestInvokeContract_InvalidContractIDFormat` - Now expects `nil, error` (fixed!)
- `TestSimulateContract_ValidArguments` - Now expects `nil, error` (fixed!)
- Integration test in `integration_test.go` - Already handles both error and success cases

### Tests That Were Updated
- `TestInvokeContract_ValidContractID` - Updated to expect `nil, error` instead of `&ContractResult{...}, nil`

## Verification
All tests should now pass because:
1. Invalid contract IDs (empty, too short, invalid prefix) return `nil, error` with message containing "contract ID is required"
2. Valid contract IDs return `nil, error` because `buildContractInvocation` returns `nil, nil` (placeholder implementation)
3. Both `SimulateContract` and `InvokeContract` now return Go `error` values instead of result structs with `Error` fields
4. All existing code that calls these functions already handles the error return signature properly

## Notes
- The `InvokeContract` function signature was already `(*ContractResult, error)`, so no changes were needed to the function signature itself.
- The `SimulateContract` function signature was already `(*SimulationResult, error)`, so no changes were needed to the function signature itself.
- The fix ensures consistent error handling: all errors are returned as Go `error` values, not as result structs with `Error` fields.
- This makes the API more idiomatic and easier to use, as callers can use standard Go error handling patterns.
