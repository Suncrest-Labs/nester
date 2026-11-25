use soroban_sdk::{
    xdr::{FromXdr, ToXdr},
    Address, Bytes, Env,
};

pub fn setup_test_env() -> Env {
    Env::default()
}

pub fn mint_test_token(_env: &Env, _amount: u128) {
    // Helper to mint test tokens
}

/// Re-create `addr` in another `Env`. Host objects are bound to the `Env`
/// that made them, so a test that restores a snapshot into a new `Env`
/// carries addresses across as XDR.
pub fn carry_address(from: &Env, to: &Env, addr: &Address) -> Address {
    let raw: std::vec::Vec<u8> = addr.clone().to_xdr(from).iter().collect();
    Address::from_xdr(to, &Bytes::from_slice(to, &raw)).unwrap()
}
