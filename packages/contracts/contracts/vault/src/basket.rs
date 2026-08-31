use soroban_sdk::{panic_with_error, Address, Env, Vec};
use nester_common::{AssetConfig, BasketValuation, ContractError, PriceInfo};

/// Maximum number of assets allowed in a multi-asset basket
pub const MAX_BASKET_SIZE: u32 = 10;

/// Maximum price age in seconds (1 hour)
pub const MAX_PRICE_AGE_SECS: u64 = 3600;

/// Maximum price deviation in basis points (10% = 1000 bps)
pub const MAX_PRICE_DEVIATION_BPS: u32 = 1000;

/// USDC has 7 decimals on Stellar
pub const USDC_DECIMALS: u32 = 7;

/// Target weights must sum to exactly 10,000 basis points
pub const TARGET_WEIGHT_SUM_BPS: u32 = 10_000;

/// Validates that asset configurations form a valid basket.
pub fn validate_basket_config(env: &Env, assets: &Vec<AssetConfig>) -> Result<(), ContractError> {
    // Check basket size limit
    if assets.len() > MAX_BASKET_SIZE {
        return Err(ContractError::ExceedsLimit);
    }

    if assets.is_empty() {
        return Err(ContractError::InvalidAmount);
    }

    // Check that target weights sum to exactly 10,000 bps
    let mut total_weight: u32 = 0;
    for asset in assets.iter() {
        total_weight = total_weight
            .checked_add(asset.target_weight_bps)
            .ok_or(ContractError::ArithmeticOverflow)?;
        
        if asset.target_weight_bps == 0 {
            return Err(ContractError::InvalidAmount);
        }
        
        if asset.max_deposit_cap <= 0 {
            return Err(ContractError::InvalidAmount);
        }
    }

    if total_weight != TARGET_WEIGHT_SUM_BPS {
        return Err(ContractError::ConfigOutOfRange);
    }

    Ok(())
}

/// Calculates the total value of a multi-asset basket.
/// Returns the total value in USDC (7 decimals) and validation status.
pub fn calculate_basket_value(
    env: &Env,
    assets: &Vec<AssetConfig>,
    balances: &Vec<i128>,
    prices: &Vec<PriceInfo>,
) -> BasketValuation {
    if assets.len() != balances.len() || assets.len() != prices.len() {
        panic_with_error!(env, ContractError::InvalidAmount);
    }

    let now = env.ledger().timestamp();
    let mut total_value: i128 = 0;
    let mut all_prices_valid = true;

    for i in 0..assets.len() {
        let asset = assets.get(i).unwrap();
        let balance = balances.get(i).unwrap();
        let price_info = prices.get(i).unwrap();

        // Check price freshness
        if now > price_info.timestamp + MAX_PRICE_AGE_SECS {
            all_prices_valid = false;
            continue;
        }

        if !price_info.is_valid {
            all_prices_valid = false;
            continue;
        }

        // Calculate value: balance * price / (10^asset_decimals)
        // Price is in USDC with 7 decimals
        let asset_value = if balance == 0 {
            0
        } else {
            let scaled_balance = balance
                .checked_div(10_i128.pow(asset.decimals))
                .unwrap_or(0);
            
            scaled_balance
                .checked_mul(price_info.price)
                .unwrap_or(0)
        };

        total_value = total_value
            .checked_add(asset_value)
            .unwrap_or_else(|| {
                panic_with_error!(env, ContractError::ArithmeticOverflow);
            });
    }

    BasketValuation {
        total_value,
        timestamp: now,
        is_valid: all_prices_valid,
    }
}

/// Checks if price deviation is within acceptable bounds.
pub fn validate_price_deviation(
    env: &Env,
    current_price: i128,
    last_price: i128,
) -> Result<(), ContractError> {
    if last_price == 0 {
        // First price update, always accept
        return Ok(());
    }

    let deviation = if current_price > last_price {
        current_price - last_price
    } else {
        last_price - current_price
    };

    let max_deviation = last_price
        .checked_mul(MAX_PRICE_DEVIATION_BPS as i128)
        .and_then(|x| x.checked_div(10_000))
        .ok_or(ContractError::ArithmeticOverflow)?;

    if deviation > max_deviation {
        return Err(ContractError::SlippageExceeded);
    }

    Ok(())
}

/// Finds an asset in the basket by its token address.
pub fn find_asset_index(assets: &Vec<AssetConfig>, token: &Address) -> Option<usize> {
    for (i, asset) in assets.iter().enumerate() {
        if &asset.token == token {
            return Some(i);
        }
    }
    None
}

/// Converts an amount from one asset's decimals to USDC decimals.
pub fn normalize_to_usdc_decimals(amount: i128, asset_decimals: u32) -> i128 {
    if asset_decimals == USDC_DECIMALS {
        return amount;
    }

    if asset_decimals > USDC_DECIMALS {
        // More decimals than USDC, divide
        let divisor = 10_i128.pow(asset_decimals - USDC_DECIMALS);
        amount.checked_div(divisor).unwrap_or(0)
    } else {
        // Fewer decimals than USDC, multiply
        let multiplier = 10_i128.pow(USDC_DECIMALS - asset_decimals);
        amount.checked_mul(multiplier).unwrap_or(i128::MAX)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use soroban_sdk::{testutils::Address as _, Env};

    #[test]
    fn test_validate_basket_config_success() {
        let env = Env::default();
        let mut assets = Vec::new(&env);
        
        assets.push_back(AssetConfig {
            token: Address::generate(&env),
            target_weight_bps: 5000,
            decimals: 7,
            price_feed_id: 1,
            max_deposit_cap: 1_000_000 * 10_i128.pow(7),
        });
        
        assets.push_back(AssetConfig {
            token: Address::generate(&env),
            target_weight_bps: 5000,
            decimals: 7,
            price_feed_id: 2,
            max_deposit_cap: 1_000_000 * 10_i128.pow(7),
        });

        assert!(validate_basket_config(&env, &assets).is_ok());
    }

    #[test]
    fn test_validate_basket_config_wrong_weight_sum() {
        let env = Env::default();
        let mut assets = Vec::new(&env);
        
        assets.push_back(AssetConfig {
            token: Address::generate(&env),
            target_weight_bps: 6000, // Wrong total (6000 + 5000 = 11000)
            decimals: 7,
            price_feed_id: 1,
            max_deposit_cap: 1_000_000 * 10_i128.pow(7),
        });
        
        assets.push_back(AssetConfig {
            token: Address::generate(&env),
            target_weight_bps: 5000,
            decimals: 7,
            price_feed_id: 2,
            max_deposit_cap: 1_000_000 * 10_i128.pow(7),
        });

        assert_eq!(
            validate_basket_config(&env, &assets).unwrap_err(),
            ContractError::ConfigOutOfRange
        );
    }

    #[test]
    fn test_calculate_basket_value() {
        let env = Env::default();
        let mut assets = Vec::new(&env);
        let mut balances = Vec::new(&env);
        let mut prices = Vec::new(&env);

        // USDC: 1000 tokens at $1.00
        assets.push_back(AssetConfig {
            token: Address::generate(&env),
            target_weight_bps: 5000,
            decimals: 7,
            price_feed_id: 1,
            max_deposit_cap: 1_000_000 * 10_i128.pow(7),
        });
        balances.push_back(1000 * 10_i128.pow(7));
        prices.push_back(PriceInfo {
            price: 1 * 10_i128.pow(7), // $1.00 with 7 decimals
            timestamp: env.ledger().timestamp(),
            is_valid: true,
        });

        // XLM: 10000 tokens at $0.10
        assets.push_back(AssetConfig {
            token: Address::generate(&env),
            target_weight_bps: 5000,
            decimals: 7,
            price_feed_id: 2,
            max_deposit_cap: 1_000_000 * 10_i128.pow(7),
        });
        balances.push_back(10000 * 10_i128.pow(7));
        prices.push_back(PriceInfo {
            price: 10_i128.pow(6), // $0.10 with 7 decimals
            timestamp: env.ledger().timestamp(),
            is_valid: true,
        });

        let valuation = calculate_basket_value(&env, &assets, &balances, &prices);
        
        // Expected: 1000 * $1.00 + 10000 * $0.10 = $2000
        assert_eq!(valuation.total_value, 2000 * 10_i128.pow(7));
        assert!(valuation.is_valid);
    }

    #[test]
    fn test_price_deviation_validation() {
        let env = Env::default();
        
        // Normal case: 5% deviation (within 10% limit)
        assert!(validate_price_deviation(&env, 105, 100).is_ok());
        
        // Edge case: exactly 10% deviation (should pass)
        assert!(validate_price_deviation(&env, 110, 100).is_ok());
        
        // Violation: 15% deviation (exceeds 10% limit)
        assert_eq!(
            validate_price_deviation(&env, 115, 100).unwrap_err(),
            ContractError::SlippageExceeded
        );
        
        // First price (no previous price)
        assert!(validate_price_deviation(&env, 100, 0).is_ok());
    }

    #[test]
    fn test_normalize_to_usdc_decimals() {
        // Same decimals (7)
        assert_eq!(normalize_to_usdc_decimals(1000_0000000, 7), 1000_0000000);
        
        // More decimals (18 -> 7, divide by 10^11)
        assert_eq!(normalize_to_usdc_decimals(1000_000000000000000000, 18), 1000_0000000);
        
        // Fewer decimals (6 -> 7, multiply by 10)
        assert_eq!(normalize_to_usdc_decimals(1000_000000, 6), 1000_0000000);
    }
}