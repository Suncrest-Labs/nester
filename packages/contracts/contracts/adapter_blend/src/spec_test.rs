//! Checks the adapter's Blend structs against Blend v2's own contract spec.
//!
//! Soroban decodes a struct only when every field name and type matches, so
//! a drift from Blend's layout is a runtime decode failure, not a compile
//! error. The v2 pool WASM targets protocol 22 and cannot run in this
//! workspace's protocol-21 host, but its spec section can still be read, so
//! each struct's spec is compared field-for-field with the pool's.

extern crate std;

use std::{string::String, vec::Vec};

use soroban_sdk::xdr::{Limits, ReadXdr, ScSpecEntry, ScSpecTypeDef, ScSpecUdtStructV0};

use crate::{BlendPositions, BlendRequest, BlendReserve, BlendReserveConfig, BlendReserveData};
use nester_test_utils::mocks::blend_pool as mock;

const BLEND_V2_POOL: &[u8] = include_bytes!("../fixtures/blend_pool_v2.wasm");

fn blend_struct(name: &str) -> ScSpecUdtStructV0 {
    soroban_spec::read::from_wasm(BLEND_V2_POOL)
        .expect("v2 pool spec")
        .into_iter()
        .find_map(|e| match e {
            ScSpecEntry::UdtStructV0(s) if s.name.to_utf8_string_lossy() == name => Some(s),
            _ => None,
        })
        .unwrap_or_else(|| panic!("v2 pool has no struct {name}"))
}

fn local_struct(xdr: &[u8]) -> ScSpecUdtStructV0 {
    match ScSpecEntry::from_xdr(xdr, Limits::none()).unwrap() {
        ScSpecEntry::UdtStructV0(s) => s,
        _ => panic!("not a struct spec"),
    }
}

/// Field names and types, with nested struct names stripped of the local
/// `Blend` prefix so `BlendReserveData` compares equal to `ReserveData`.
fn fields(s: &ScSpecUdtStructV0) -> Vec<(String, String)> {
    s.fields
        .iter()
        .map(|f| {
            let ty = match &f.type_ {
                ScSpecTypeDef::Udt(u) => {
                    let n = u.name.to_utf8_string_lossy();
                    n.strip_prefix("Blend").unwrap_or(&n).into()
                }
                other => std::format!("{other:?}"),
            };
            (f.name.to_utf8_string_lossy(), ty)
        })
        .collect()
}

fn assert_matches(local_xdr: &[u8], blend_name: &str) {
    assert_eq!(
        fields(&local_struct(local_xdr)),
        fields(&blend_struct(blend_name)),
        "{blend_name} does not match Blend v2's spec"
    );
}

#[test]
fn adapter_structs_match_blend_v2_spec() {
    assert_matches(&BlendReserve::spec_xdr(), "Reserve");
    assert_matches(&BlendReserveConfig::spec_xdr(), "ReserveConfig");
    assert_matches(&BlendReserveData::spec_xdr(), "ReserveData");
    assert_matches(&BlendRequest::spec_xdr(), "Request");
    assert_matches(&BlendPositions::spec_xdr(), "Positions");
}

#[test]
fn mock_pool_structs_match_blend_v2_spec() {
    assert_matches(&mock::Reserve::spec_xdr(), "Reserve");
    assert_matches(&mock::ReserveConfig::spec_xdr(), "ReserveConfig");
    assert_matches(&mock::ReserveData::spec_xdr(), "ReserveData");
    assert_matches(&mock::Request::spec_xdr(), "Request");
    assert_matches(&mock::Positions::spec_xdr(), "Positions");
}
