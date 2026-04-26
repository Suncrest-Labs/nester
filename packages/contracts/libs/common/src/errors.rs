use soroban_sdk::contracterror;

#[contracterror]
#[derive(Copy, Clone, Debug, Eq, PartialEq, PartialOrd, Ord)]
pub enum ContractError {
    AlreadyInitialized = 1,
    NotInitialized = 2,
    Unauthorized = 3,
    InsufficientBalance = 4,
    InvalidAmount = 5,
    StrategyNotFound = 6,
    AllocationError = 7,
    RoleNotFound = 8,
    InvalidOperation = 9,
    ExceedsLimit = 10,
    CircuitBreakerTriggered = 11,
    TimelockNotReady = 12,
    TimelockExpired = 13,
    TimelockNotFound = 14,
    TimelockInvalidDelay = 15,
    TimelockAlreadyExecuted = 16,
    SlippageExceeded = 17,
    ArithmeticOverflow = 18,
}
