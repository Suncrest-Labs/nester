//! Checks the mock vault's functions and types against the DeFindex vault's
//! own contract spec.
//!
//! The adapter calls the vault by name with positional arguments, so a
//! signature drift is a runtime failure, not a compile error. The adapter's
//! tests run against `MockDefindexVault`; this test pins that mock to the
//! deployed testnet vault's contract spec, extracted from its WASM (see
//! `fixtures/README.md`).

extern crate std;

use std::{format, string::String, vec::Vec};

use soroban_sdk::xdr::{
    Limits, ReadXdr, ScSpecEntry, ScSpecFunctionV0, ScSpecTypeDef, ScSpecUdtStructV0,
};

use nester_test_utils::mocks::defindex_vault::{
    AssetInvestmentAllocation, MockDefindexVault, StrategyAllocation,
};

const DEFINDEX_VAULT_SPEC: &[u8] = include_bytes!("../fixtures/defindex_vault.spec.xdr");

fn vault_spec() -> Vec<ScSpecEntry> {
    soroban_spec::read::parse_raw(DEFINDEX_VAULT_SPEC).expect("vault spec")
}

fn vault_fn(name: &str) -> ScSpecFunctionV0 {
    vault_spec()
        .into_iter()
        .find_map(|e| match e {
            ScSpecEntry::FunctionV0(f) if f.name.to_utf8_string_lossy() == name => Some(f),
            _ => None,
        })
        .unwrap_or_else(|| panic!("vault has no fn {name}"))
}

fn vault_struct(name: &str) -> ScSpecUdtStructV0 {
    vault_spec()
        .into_iter()
        .find_map(|e| match e {
            ScSpecEntry::UdtStructV0(s) if s.name.to_utf8_string_lossy() == name => Some(s),
            _ => None,
        })
        .unwrap_or_else(|| panic!("vault has no struct {name}"))
}

fn local(xdr: &[u8]) -> ScSpecEntry {
    ScSpecEntry::from_xdr(xdr, Limits::none()).unwrap()
}

/// Input names and types, then output types. UDTs compare by name.
fn signature(f: &ScSpecFunctionV0) -> (Vec<(String, String)>, Vec<String>) {
    let inputs = f
        .inputs
        .iter()
        .map(|i| (i.name.to_utf8_string_lossy(), format!("{:?}", i.type_)))
        .collect();
    let outputs = f
        .outputs
        .iter()
        .map(|t: &ScSpecTypeDef| format!("{t:?}"))
        .collect();
    (inputs, outputs)
}

fn assert_fn_matches(local_xdr: &[u8], name: &str) {
    let ScSpecEntry::FunctionV0(mock) = local(local_xdr) else {
        panic!("not a function spec");
    };
    assert_eq!(
        signature(&mock),
        signature(&vault_fn(name)),
        "{name} does not match the DeFindex vault's spec"
    );
}

fn fields(s: &ScSpecUdtStructV0) -> Vec<(String, String)> {
    s.fields
        .iter()
        .map(|f| (f.name.to_utf8_string_lossy(), format!("{:?}", f.type_)))
        .collect()
}

fn assert_struct_matches(local_xdr: &[u8], name: &str) {
    let ScSpecEntry::UdtStructV0(mock) = local(local_xdr) else {
        panic!("not a struct spec");
    };
    assert_eq!(
        fields(&mock),
        fields(&vault_struct(name)),
        "{name} does not match the DeFindex vault's spec"
    );
}

#[test]
fn mock_vault_functions_match_defindex_spec() {
    assert_fn_matches(&MockDefindexVault::spec_xdr_deposit(), "deposit");
    assert_fn_matches(&MockDefindexVault::spec_xdr_withdraw(), "withdraw");
    assert_fn_matches(
        &MockDefindexVault::spec_xdr_get_asset_amounts_per_shares(),
        "get_asset_amounts_per_shares",
    );
    assert_fn_matches(&MockDefindexVault::spec_xdr_balance(), "balance");
    assert_fn_matches(&MockDefindexVault::spec_xdr_total_supply(), "total_supply");
}

#[test]
fn mock_vault_types_match_defindex_spec() {
    assert_struct_matches(
        &AssetInvestmentAllocation::spec_xdr(),
        "AssetInvestmentAllocation",
    );
    assert_struct_matches(&StrategyAllocation::spec_xdr(), "StrategyAllocation");
}
