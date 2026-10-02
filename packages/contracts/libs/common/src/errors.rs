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
    QueueEntryExists = 24,
    RequestBelowMinimum = 25,
    NotInQueue = 26,
    LegSlippageExceeded = 27,
    PlanStale = 28,
    RebalanceValueCapExceeded = 29,
    RoleRequired = 30,
    RoleTransferNotPending = 31,
    RoleExpired = 32,
    AlreadyReferred = 33,
    SelfReferral = 34,
    ReferralCycle = 35,
    BudgetExhausted = 36,
    BelowClaimMinimum = 37,
    RecoveryCooldownActive = 38,
    RecoveryStageInvalid = 39,
    NotNesterVault = 40,
    VaultCreationFailed = 41,
    UpgradeNotMatured = 42,
    NoPendingUpgrade = 43,
    UpgradeHashMismatch = 44,
    SchemaVersionMismatch = 45,
    // --- #808: Recurring Deposit Mandate errors ---
    // MandateNotFound -> reuse StrategyNotFound (6)
    // MandateInactive -> reuse InvalidOperation (9)
    // MandatePaused -> reuse InvalidOperation (9)
    // MandateNotDue -> reuse TimelockNotReady (12)
    // MandateExpired -> reuse TimelockExpired (13)
    // MandateExhausted -> reuse BudgetExhausted (36)
    // --- #804: Multi-Asset Vault errors ---
    // PriceStale -> reuse TimelockExpired (13)
    // PriceUnavailable -> reuse StrategyNotFound (6)
    // AssetNotInBasket -> reuse StrategyNotFound (6)
    // PriceDeviationExceeded -> reuse SlippageExceeded (17)
    // Attestation errors (issue #820 — signature-attested APY/TVL updates).
    // Numbered after the base branch's highest rather than at 36-40 as this
    // branch originally had them: discriminants are the on-chain error codes
    // clients match on, so the existing ones must not shift.
    AttesterNotRegistered = 46,
    SignatureInvalid = 47,
    AttestationExpired = 48,
    NonceReused = 49,
    ThresholdNotMet = 50,
    // --- #802: Time-locked savings vault errors ---
    // This enum is already at Soroban's hard cap of 50 error variants
    // (stellar-xdr's ScSpecUdtErrorEnumV0.cases is VecM<_, 50>, enforced by
    // #[contracterror] at compile time via a `.try_into().unwrap()` that
    // panics with `LengthExceedsMax` past 50) — there is no room to add any
    // more, contrary to what issue #802 itself asks for. Every new failure
    // mode below is mapped onto the closest existing variant instead, same
    // pattern as the #808/#804 reuse table above:
    // LockNotMatured -> reuse TimelockNotReady (12)
    // InvalidLockDuration -> reuse ConfigOutOfRange (19)
    // LockNotFound -> reuse TimelockNotFound (14)
    // InsufficientFlexibleShares -> reuse InsufficientBalance (4)
    // LockLimitExceeded (per-user open-lock cap) -> reuse ExceedsLimit (10)
}

// Alias for backwards compatibility
pub type Error = ContractError;
