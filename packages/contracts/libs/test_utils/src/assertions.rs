use nester_common::ContractError;

pub fn assert_error(result: Result<(), ContractError>, expected: ContractError) {
    match result {
        Ok(_) => panic!("Expected error but succeeded"),
        Err(e) => assert_eq!(e, expected),
    }
}

pub fn assert_ok<T>(result: Result<T, ContractError>) {
    assert!(result.is_ok(), "Expected Ok but got error");
}

pub fn assert_eq_balance(actual: u128, expected: u128) {
    assert_eq!(actual, expected, "Balance mismatch");
}

pub fn assert_reentrancy_blocked(result: &core::result::Result<(), soroban_sdk::Error>) {
    assert!(result.is_err(), "expected reentrancy to be blocked");
}
