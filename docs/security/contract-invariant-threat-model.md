# Threat Model: Bounded Exhaustive Checking

This document details the exact invariants enforced on the Nester protocol contracts, the attacks they prevent, and the limitations of bounded checking.

## 1. Conservation of Assets
**Predicate:** `sum(user_balances) * share_price == total_assets +/- rounding_bound`
**Attack Prevented:** Silent insolvency or inflation. It guarantees no user can extract more assets than their fair share, preventing draining attacks.

## 2. Share Price Monotonicity
**Predicate:** `share_price_t_n >= share_price_t_{n-1}`
**Attack Prevented:** Internal dilution or value extraction. Specifically prevents attacks like `#1029`, where a performance fee charged against the vault reduces the gross value but fails to adjust the token supply proportionally, effectively stealing value from remaining depositors.

## 3. Authorization
**Predicate:** `state_mutation_x => auth(required_role)`
**Attack Prevented:** Unauthorized protocol manipulation, such as changing critical vault parameters, registering malicious adapters, or stealing funds by calling unauthenticated entry points.

## 4. Deviation Bounds
**Predicate:** `abs(APY_new - APY_old) > deviation => APY_state = APY_old AND failure_count++`
**Attack Prevented:** Oracle manipulation or adapter compromise. If an adapter tries to report an anomalous 1,000,000% APY, the protocol explicitly rejects it and records a failure, eventually quarantining the source.

## 5. Fee Bounds
**Predicate:** `performance_fee > 0 => current_value > high_water_mark AND performance_fee == (current_value - high_water_mark) * rate`
**Attack Prevented:** Fee extraction on principal or fake yield. Prevents the treasury from profiting off user principal or during a recovery from an adapter loss.

## 6. Liveness of Degradation
**Predicate:** `status == Degraded => next_state != Active UNLESS explicit_recovery_action`
**Attack Prevented:** Silent resurrection of compromised adapters. A flapping adapter that repeatedly fails cannot silently re-enter active rotation without human admin intervention, as demonstrated by the fix to `#1030` where `failure_count` resets were improperly guarding the degradation threshold.

## Limits of Bounded Exhaustive Checking
> [!WARNING]
> Bounded exhaustive exploration (depth `N = 5`) **does not constitute a mathematical proof of correctness**.
> 
> * **Long Sequences:** Attacks requiring 6 or more steps to manifest are invisible to this checker.
> * **Complex Adversarial Ordering:** Flash loans, sandwich attacks across multiple external contracts, or cross-contract reentrancy might require specific host mock setups that this harness does not explore.
> * **Economic Attacks:** Attacks that exploit the economic realities (e.g., oracle liquidity) rather than the contract logic are not detected by state machine execution alone.
>
> Overstating the guarantee of this tool creates a false sense of security. It is a highly effective bug-finding tool, but human diligence and economic modeling remain essential.
