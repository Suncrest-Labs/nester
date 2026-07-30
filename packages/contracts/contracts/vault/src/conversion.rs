//! Pure tokenised-vault conversion arithmetic.
//!
//! # Rounding policy
//!
//! * Conversions determining what a user **receives** round down.
//! * Conversions determining what a user **pays** round up.
//! * Every remainder therefore favours the vault, never the user.
//!
//! This rule is security-critical: rounding in the user's favour can allow
//! repeated small operations to inflate shares or drain underlying assets.

use nester_common::ContractError;

/// Computes `floor(x * y / denominator)` without constructing the potentially
/// overflowing intermediate product `x * y`.
pub fn mul_div_down(x: i128, y: i128, denominator: i128) -> Result<i128, ContractError> {
    mul_div(x, y, denominator).map(|(quotient, _)| quotient)
}

/// Computes `ceil(x * y / denominator)` without constructing the potentially
/// overflowing intermediate product `x * y`.
pub fn mul_div_up(x: i128, y: i128, denominator: i128) -> Result<i128, ContractError> {
    let (quotient, remainder) = mul_div(x, y, denominator)?;
    if remainder == 0 {
        Ok(quotient)
    } else {
        quotient
            .checked_add(1)
            .ok_or(ContractError::ArithmeticOverflow)
    }
}

/// Shares received for an asset amount.
pub fn assets_to_shares_down(
    assets: i128,
    total_assets: i128,
    total_shares: i128,
) -> Result<i128, ContractError> {
    validate_conversion_inputs(assets, total_assets, total_shares)?;
    if total_shares == 0 {
        return Ok(assets);
    }
    // A live supply backed by zero assets is insolvent. Issuing shares 1:1 in
    // this state would let a new depositor recapitalise existing holders and
    // receive an incorrect ownership fraction.
    if total_assets == 0 {
        return Err(ContractError::InvalidOperation);
    }
    mul_div_down(assets, total_shares, total_assets)
}

/// Shares a user must pay for an exact asset amount.
pub fn assets_to_shares_up(
    assets: i128,
    total_assets: i128,
    total_shares: i128,
) -> Result<i128, ContractError> {
    validate_conversion_inputs(assets, total_assets, total_shares)?;
    if total_shares == 0 {
        return Ok(assets);
    }
    if total_assets == 0 {
        return Err(ContractError::InvalidOperation);
    }
    mul_div_up(assets, total_shares, total_assets)
}

/// Assets received for a share amount.
pub fn shares_to_assets_down(
    shares: i128,
    total_assets: i128,
    total_shares: i128,
) -> Result<i128, ContractError> {
    validate_conversion_inputs(shares, total_assets, total_shares)?;
    if total_shares == 0 {
        return Ok(shares);
    }
    mul_div_down(shares, total_assets, total_shares)
}

/// Assets a user must pay for an exact share amount.
pub fn shares_to_assets_up(
    shares: i128,
    total_assets: i128,
    total_shares: i128,
) -> Result<i128, ContractError> {
    validate_conversion_inputs(shares, total_assets, total_shares)?;
    if total_shares == 0 {
        return Ok(shares);
    }
    mul_div_up(shares, total_assets, total_shares)
}

fn validate_conversion_inputs(
    amount: i128,
    total_assets: i128,
    total_shares: i128,
) -> Result<(), ContractError> {
    if amount < 0 || total_assets < 0 || total_shares < 0 {
        return Err(ContractError::InvalidAmount);
    }
    Ok(())
}

/// Binary long multiplication represented as `(quotient, remainder)` modulo
/// `denominator`. This keeps every intermediate within `i128` while preserving
/// the exact mathematical result.
fn mul_div(x: i128, y: i128, denominator: i128) -> Result<(i128, i128), ContractError> {
    if x < 0 || y < 0 || denominator <= 0 {
        return Err(ContractError::InvalidAmount);
    }
    if x == 0 || y == 0 {
        return Ok((0, 0));
    }

    let mut bits = x;
    let mut part_q = y / denominator;
    let mut part_r = y % denominator;
    let mut result_q = 0_i128;
    let mut result_r = 0_i128;

    while bits != 0 {
        if bits & 1 == 1 {
            result_q = result_q
                .checked_add(part_q)
                .ok_or(ContractError::ArithmeticOverflow)?;
            if result_r >= denominator - part_r {
                result_r -= denominator - part_r;
                result_q = result_q
                    .checked_add(1)
                    .ok_or(ContractError::ArithmeticOverflow)?;
            } else {
                result_r += part_r;
            }
        }

        bits >>= 1;
        if bits == 0 {
            break;
        }

        part_q = part_q
            .checked_mul(2)
            .ok_or(ContractError::ArithmeticOverflow)?;
        if part_r >= denominator - part_r {
            part_r -= denominator - part_r;
            part_q = part_q
                .checked_add(1)
                .ok_or(ContractError::ArithmeticOverflow)?;
        } else {
            part_r += part_r;
        }
    }

    Ok((result_q, result_r))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn checked_mul_div_handles_product_larger_than_i128() {
        assert_eq!(mul_div_down(i128::MAX, i128::MAX, i128::MAX), Ok(i128::MAX));
    }

    #[test]
    fn insolvent_live_supply_never_uses_bootstrap_rate() {
        assert_eq!(
            assets_to_shares_down(100, 0, 50),
            Err(ContractError::InvalidOperation)
        );
        assert_eq!(
            assets_to_shares_up(100, 0, 50),
            Err(ContractError::InvalidOperation)
        );
    }
}
