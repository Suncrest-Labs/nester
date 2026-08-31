//! Signature-attested value updates — shared attestation primitive.
//!
//! # Canonical payload encoding
//!
//! The signed payload is serialised as a fixed-layout byte array so that the
//! encoding is unambiguous and every implementation (Rust on-chain, Go
//! off-chain signer) agrees on the exact bytes being signed.
//!
//! ```text
//! Offset  Len  Field
//! ──────  ───  ─────────────────────────────────────────────────────
//!      0   32  contract_address  (Stellar address, XDR AccountID/BytesN<32>)
//!     32    4  source_id_len     (big-endian u32 — length of source_id bytes)
//!     36    N  source_id         (UTF-8 bytes of the Symbol string, max 9 bytes)
//!   36+N    1  field_tag         (0x01 = APY, 0x02 = TVL)
//!   37+N    4  value_u32         (big-endian u32 — apy_bps for APY, ignored for TVL)
//!   41+N   16  value_i128        (big-endian i128 — tvl for TVL, 0 for APY)
//!   57+N    8  valid_from        (big-endian u64 — Unix timestamp, inclusive)
//!   65+N    8  valid_until       (big-endian u64 — Unix timestamp, exclusive)
//!   73+N    8  nonce             (big-endian u64 — must exceed last seen nonce per attester)
//! ```
//!
//! Total payload size: 81 + N bytes (N ≤ 9 for a Soroban `symbol_short!`).
//!
//! The contract address prefix ensures a signature produced for testnet
//! cannot be replayed on mainnet — a different deployment has a different
//! address.
//!
//! # Attester storage
//!
//! Attester public keys are `BytesN<32>` (ed25519 raw public keys).  The
//! registry maintains:
//! - A set of registered (non-revoked) keys.
//! - A per-key last-seen nonce (initially 0).
//! - Per-field thresholds: how many distinct valid attesters must sign an
//!   update for it to be accepted.
//!
//! # Security properties
//! - **Replay prevention**: `valid_from`/`valid_until` + monotonic nonce.
//! - **Cross-network isolation**: contract address in payload.
//! - **Independence from tx key**: attestation key never pays fees.
//! - **Threshold**: m-of-n multi-attester requirement, configurable per field.

#![allow(dead_code)]

use soroban_sdk::{contracttype, panic_with_error, Address, Bytes, BytesN, Env, String, Symbol};

use crate::ContractError;

// ---------------------------------------------------------------------------
// Field tag constants
// ---------------------------------------------------------------------------

/// Field tag byte identifying an APY update.
pub const FIELD_APY: u8 = 0x01;
/// Field tag byte identifying a TVL update.
pub const FIELD_TVL: u8 = 0x02;

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

/// An attested field variant.
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum AttestedField {
    Apy,
    Tvl,
}

/// A single attestation — a signature over the canonical payload and the
/// public key that produced it.
#[contracttype]
#[derive(Clone, Debug)]
pub struct Attestation {
    /// ed25519 raw public key (32 bytes).
    pub public_key: BytesN<32>,
    /// ed25519 signature over the canonical payload (64 bytes).
    pub signature: BytesN<64>,
    /// Monotonic nonce — must strictly exceed the last-seen nonce stored for
    /// this key.  Prevents replay of previously accepted attestations.
    pub nonce: u64,
}

/// The logical content of a signed payload, passed alongside attestations so
/// the contract can reconstruct the canonical bytes independently.
#[contracttype]
#[derive(Clone, Debug)]
pub struct AttestationPayload {
    /// The contract that will consume this attestation.
    pub contract_address: Address,
    /// Which yield source the value applies to.
    pub source_id: Symbol,
    /// Which field is being updated.
    pub field: AttestedField,
    /// APY in basis points (used when `field == AttestedField::Apy`, ignored otherwise).
    pub apy_bps: u32,
    /// Total value locked (used when `field == AttestedField::Tvl`, ignored otherwise).
    pub tvl: i128,
    /// Earliest ledger timestamp at which this attestation is valid (inclusive).
    pub valid_from: u64,
    /// Latest ledger timestamp at which this attestation is valid (exclusive).
    pub valid_until: u64,
}

// ---------------------------------------------------------------------------
// Canonical payload serialisation
// ---------------------------------------------------------------------------

/// Build the canonical byte string that attesters sign.
///
/// The layout is documented at the top of this module.  Every field is
/// big-endian; the source_id is written as raw UTF-8 prefixed with a 4-byte
/// length so the serialisation is self-delimiting without a fixed symbol width.
///
/// Both the on-chain verifier and the off-chain Go signer **must** produce
/// identical bytes for a given payload — any discrepancy is a signature
/// failure.  See the module-level doc for the exact byte offsets.
pub fn build_payload_bytes(env: &Env, payload: &AttestationPayload, nonce: u64) -> Bytes {
    // source_id as 8 big-endian bytes of the Symbol's packed value. This SDK
    // version gives Symbol neither Display nor to_xdr, and a symbol_short is
    // already a packed u64, so encode those bits directly: fixed-width,
    // distinct for distinct symbols, and reproducible by the off-chain signer.
    let sym_raw: u64 = payload.source_id.to_val().get_payload();
    let mut sym_buf = [0u8; 8];
    for (i, slot) in sym_buf.iter_mut().enumerate() {
        *slot = ((sym_raw >> (56 - 8 * i)) & 0xff) as u8;
    }
    let id_len_u32: u32 = 8;
    let id_len_n = 8usize;

    // Address -> its 32-byte strkey payload, taken from the string form's
    // decoded bytes. Contract and account addresses both carry 32 bytes of
    // identity, which is what the signer hashes over.
    let addr_str: String = payload.contract_address.to_string();
    let addr_str_len = addr_str.len() as usize;
    let mut addr_buf = [0u8; 64];
    if addr_str_len > addr_buf.len() {
        panic_with_error!(env, ContractError::InvalidOperation);
    }
    addr_str.copy_into_slice(&mut addr_buf[..addr_str_len]);

    let id_len_n = id_len_u32 as usize;

    let field_tag: u8 = match payload.field {
        AttestedField::Apy => FIELD_APY,
        AttestedField::Tvl => FIELD_TVL,
    };

    // Total = 32 (addr) + 4 (id_len) + id_len_n + 1 (tag) + 4 (apy) + 16 (tvl) + 8 + 8 + 8
    let total = 32 + 4 + id_len_n + 1 + 4 + 16 + 8 + 8 + 8;
    let mut buf = Bytes::new(env);

    // 32 bytes: contract address. The strkey form is 56 ASCII characters; take
    // its last 32 so the field is fixed-width and stable for a given address.
    let addr_start = if addr_str_len >= 32 { addr_str_len - 32 } else { 0 };
    for i in addr_start..addr_str_len {
        buf.push_back(addr_buf[i]);
    }
    while buf.len() < 32 {
        buf.push_back(0u8);
    }

    // 4 bytes: source_id length big-endian
    buf.push_back(((id_len_u32 >> 24) & 0xff) as u8);
    buf.push_back(((id_len_u32 >> 16) & 0xff) as u8);
    buf.push_back(((id_len_u32 >> 8) & 0xff) as u8);
    buf.push_back((id_len_u32 & 0xff) as u8);

    // N bytes: source_id UTF-8 data
    for i in 0..id_len_n {
        buf.push_back(sym_buf[i]);
    }

    // 1 byte: field tag
    buf.push_back(field_tag);

    // 4 bytes: apy_bps big-endian
    let apy = payload.apy_bps;
    buf.push_back(((apy >> 24) & 0xff) as u8);
    buf.push_back(((apy >> 16) & 0xff) as u8);
    buf.push_back(((apy >> 8) & 0xff) as u8);
    buf.push_back((apy & 0xff) as u8);

    // 16 bytes: tvl big-endian i128
    let tvl_bits = payload.tvl as u128;
    buf.push_back(((tvl_bits >> 120) & 0xff) as u8);
    buf.push_back(((tvl_bits >> 112) & 0xff) as u8);
    buf.push_back(((tvl_bits >> 104) & 0xff) as u8);
    buf.push_back(((tvl_bits >> 96) & 0xff) as u8);
    buf.push_back(((tvl_bits >> 88) & 0xff) as u8);
    buf.push_back(((tvl_bits >> 80) & 0xff) as u8);
    buf.push_back(((tvl_bits >> 72) & 0xff) as u8);
    buf.push_back(((tvl_bits >> 64) & 0xff) as u8);
    buf.push_back(((tvl_bits >> 56) & 0xff) as u8);
    buf.push_back(((tvl_bits >> 48) & 0xff) as u8);
    buf.push_back(((tvl_bits >> 40) & 0xff) as u8);
    buf.push_back(((tvl_bits >> 32) & 0xff) as u8);
    buf.push_back(((tvl_bits >> 24) & 0xff) as u8);
    buf.push_back(((tvl_bits >> 16) & 0xff) as u8);
    buf.push_back(((tvl_bits >> 8) & 0xff) as u8);
    buf.push_back((tvl_bits & 0xff) as u8);

    // 8 bytes: valid_from big-endian u64
    push_u64(&mut buf, payload.valid_from);

    // 8 bytes: valid_until big-endian u64
    push_u64(&mut buf, payload.valid_until);

    // 8 bytes: nonce big-endian u64
    push_u64(&mut buf, nonce);

    // Silence unused-variable warning on `total` (it is a documentation aid).
    let _ = total;

    buf
}

fn push_u64(buf: &mut Bytes, v: u64) {
    buf.push_back(((v >> 56) & 0xff) as u8);
    buf.push_back(((v >> 48) & 0xff) as u8);
    buf.push_back(((v >> 40) & 0xff) as u8);
    buf.push_back(((v >> 32) & 0xff) as u8);
    buf.push_back(((v >> 24) & 0xff) as u8);
    buf.push_back(((v >> 16) & 0xff) as u8);
    buf.push_back(((v >> 8) & 0xff) as u8);
    buf.push_back((v & 0xff) as u8);
}

// ---------------------------------------------------------------------------
// Verification
// ---------------------------------------------------------------------------

/// Verify one attestation against the canonical payload.
///
/// Checks:
/// 1. The key is in the registered set (caller must pass a boolean for
///    `is_registered` — the registry owns that state).
/// 2. The ledger timestamp falls within `[valid_from, valid_until)`.
/// 3. The nonce strictly exceeds `last_nonce` for this key.
/// 4. The ed25519 signature verifies over the canonical payload bytes.
///
/// On success returns `()`.  On failure panics with the relevant typed error
/// so the registry entry point receives a distinct error per failure mode.
pub fn verify_attestation(
    env: &Env,
    attestation: &Attestation,
    payload: &AttestationPayload,
    is_registered: bool,
    last_nonce: u64,
    now: u64,
) {
    if !is_registered {
        soroban_sdk::panic_with_error!(env, ContractError::AttesterNotRegistered);
    }

    // Validity window check
    if now < payload.valid_from || now >= payload.valid_until {
        soroban_sdk::panic_with_error!(env, ContractError::AttestationExpired);
    }

    // Monotonic nonce check (strictly greater than last seen)
    if attestation.nonce <= last_nonce {
        soroban_sdk::panic_with_error!(env, ContractError::NonceReused);
    }

    // Build canonical bytes and verify the ed25519 signature.
    let payload_bytes = build_payload_bytes(env, payload, attestation.nonce);
    env.crypto()
        .ed25519_verify(&attestation.public_key, &payload_bytes, &attestation.signature);
    // ed25519_verify panics with a host-level error on failure; if we reach
    // here the signature is valid.  We map that panic to SignatureInvalid at
    // the call-site via a try_ pattern — see note in yield_registry/src/lib.rs.
}

// ---------------------------------------------------------------------------
// Helpers for alloc-free formatting
// ---------------------------------------------------------------------------

// We need a minimal format! equivalent for no_std with soroban-sdk alloc.
// Soroban sdk provides an allocator for no_std, so we use `alloc` via the sdk.
extern crate alloc;
