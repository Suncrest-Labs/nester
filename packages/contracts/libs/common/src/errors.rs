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
    FeeTooHigh = 18,
    ConfigOutOfRange = 19,
    ArithmeticOverflow = 20,
    BelowMinDeposit = 21,
    ReentrancyDetected = 22,
    CalleeNotAllowed = 23,
    RoleRequired = 24,
    RoleTransferNotPending = 25,
    RoleExpired = 26,
    AlreadyReferred = 27,
    SelfReferral = 28,
    ReferralCycle = 29,
    BudgetExhausted = 30,
    BelowClaimMinimum = 31,
    RecoveryCooldownActive = 32,
    RecoveryStageInvalid = 33,
    NotNesterVault = 34,
    VaultCreationFailed = 35,
    // Attestation errors (issue #820 — signature-attested APY/TVL updates)
    /// The supplied public key is not in the registered attester set, or has
    /// been revoked.
    AttesterNotRegistered = 36,
    /// The ed25519 signature does not verify over the canonical payload bytes.
    SignatureInvalid = 37,
    /// The ledger timestamp falls outside the attestation's `[valid_from,
    /// valid_until)` window.
    AttestationExpired = 38,
    /// The supplied nonce is not strictly greater than the last nonce recorded
    /// for this attester key — replay or out-of-order submission.
    NonceReused = 39,
    /// Fewer distinct valid attesters signed than the configured threshold for
    /// this field.
    ThresholdNotMet = 40,
}
