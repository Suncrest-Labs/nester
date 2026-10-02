# Yield Source Adapter Interface Contract

This document describes the interface every yield source adapter (`adapter_blend`,
`adapter_soroswap`, and any future protocol adapter) must implement, so a new
adapter can be built directly against this spec instead of reverse-engineering
it from the existing Blend/Soroswap source.

## Why adapters exist

The vault understands exactly one protocol-agnostic trait, `YieldAdapter`
(`packages/contracts/libs/common/src/adapters.rs`). Each external yield venue
(a Blend lending pool, a Soroswap pair, ...) gets its own small adapter
contract translating between that venue's real interface and this trait.
Adding a third protocol means writing one small adapter contract; the vault
itself never changes.

Adapters are the least-trusted contracts in the system: they are the only
contracts that talk to third-party protocol code the vault does not control.
Keep them small and auditable, and treat every adapter call as fallible.

## The `YieldAdapter` trait

```rust
pub trait YieldAdapter {
    fn deposit(env: Env, from: Address, amount: i128, min_units_out: i128) -> i128;
    fn withdraw(env: Env, to: Address, units: i128, min_out: i128) -> i128;
    fn position_value(env: Env, owner: Address) -> i128;
    fn current_apy(env: Env) -> AdapterApy;
    fn underlying(env: Env) -> Address;
    fn max_deposit(env: Env) -> i128;
    fn max_withdraw(env: Env) -> i128;
}
```

Defined once in `nester_common` and re-exported via `#[contractclient(name = "YieldAdapterClient")]`,
so the vault calls every adapter through the same generated client regardless
of which protocol is behind it.

### `deposit`

```rust
fn deposit(env: Env, from: Address, amount: i128, min_units_out: i128) -> i128;
```

Deposits `amount` of the underlying asset into the protocol on the vault's
behalf. Returns the position units received.

**Value-transfer contract: authorized pull.** The adapter debits `amount` of
the underlying from `from` *inside this invocation*, not before it. The
caller must pre-authorize exactly that transfer, a contract caller via
`authorize_as_current_contract` scoped to this token, this amount, and
nothing else. Pulling inside the call (rather than being pushed beforehand)
is what makes a failed deposit atomic: if the adapter or the underlying
protocol reverts, the transfer rolls back with it and no funds are stranded
at the adapter.

Reverts with `SlippageExceeded` (error code 17, see [Errors](#errors) below)
if the units actually received would be below `min_units_out`.

### `withdraw`

```rust
fn withdraw(env: Env, to: Address, units: i128, min_out: i128) -> i128;
```

Withdraws `units` of the adapter's position, sending the underlying asset to
`to`. Returns the assets received.

Reverts with `SlippageExceeded` if the assets actually received would be
below `min_out`.

### `position_value`

```rust
fn position_value(env: Env, owner: Address) -> i128;
```

Current asset-denominated value of the adapter's aggregate position. Read-only.

### `current_apy`

```rust
fn current_apy(env: Env) -> AdapterApy;
```

```rust
pub enum ApyConfidence {
    ProtocolReported, // read directly from the protocol; authoritative
    Derived,           // derived from observed position growth over a sufficient window
    Unavailable,       // no meaningful rate yet
}

pub struct AdapterApy {
    pub apy_bps: u32,
    pub confidence: ApyConfidence,
}
```

`Unavailable` is deliberately distinct from a zero APY. A derived APY over a
short or brand-new position is noise, and feeding noise into
auto-rebalancing churns the vault into fee waste. **Callers must ignore
`apy_bps` whenever `confidence == Unavailable`** rather than treating it as
"the rate is zero."

Which confidence level an adapter reports depends on what the underlying
protocol exposes:
- If the protocol publishes a rate directly (e.g. reserve data), report
  `ProtocolReported`.
- If the protocol publishes no rate (e.g. a bare AMM pair), derive one from
  observed position growth and report `Derived`.
- Before enough history exists to derive a rate, report `Unavailable`
  rather than guessing.

### `underlying`

```rust
fn underlying(env: Env) -> Address;
```

The single underlying asset this adapter accepts and pays out. Read-only,
set once at initialization (see [Initialization](#initialization)).

### `max_deposit` / `max_withdraw`

```rust
fn max_deposit(env: Env) -> i128;
fn max_withdraw(env: Env) -> i128;
```

The protocol-side ceiling on how much can currently be deposited, and the
protocol-side maximum currently withdrawable, in position units. `0` means
no capacity right now, not "unimplemented" or "unlimited." The vault uses
these to avoid a `deposit`/`withdraw` call it can already tell will fail or
be partially filled by the underlying protocol.

## Initialization

`initialize` is **not** part of `YieldAdapter`, because every protocol needs
different constructor arguments (a Blend adapter needs the pool address and
reserve index; a Soroswap adapter needs the router, pair, and paired asset
address). Each adapter defines its own `initialize` function instead,
following this shared pattern:

- Require auth from the `vault` address being registered as the owner:
  `vault.require_auth()`.
- Guard against re-initialization: panic with `ContractError::AlreadyInitialized`
  if instance storage already has a `Vault` key set.
- Persist every constructor argument to instance storage.
- Initialize the adapter's position-units counter to `0`.

A new adapter should follow this exact shape rather than inventing its own
initialization convention, so every adapter's constructor is predictable
from the outside even though the argument list differs.

## Events

Every `deposit` and `withdraw` call emits an event on success, documented in
full in [`EVENTS.md`](./EVENTS.md#adapter-deposit--withdraw):

- **Topics**: `(ADAPTER, DEPOSIT | WITHDRAW, counterparty: Address)`
- **Data**: `{ amount: i128, units: i128 }` — `amount` is underlying assets
  moved in/out, `units` is protocol position units minted/burned.

Each adapter defines its own local `AdapterMovedEventData` struct with this
exact shape (`{ amount: i128, units: i128 }`) rather than importing a shared
type from `nester_common` — match this shape exactly rather than
introducing a different field set or ordering, since the vault and any
off-chain indexer decode this event by its documented shape, not by type
identity. Emit through the shared `nester_common::emit_event` helper using
the same `ADAPTER` contract symbol and `DEPOSIT`/`WITHDRAW` action symbols
every existing adapter uses, so a new adapter's events are indistinguishable
in shape from Blend's or Soroswap's, only the `counterparty` address and
data values differ per call.

## Errors

Adapters share the single `ContractError` enum from `nester_common`
(`packages/contracts/libs/common/src/errors.rs`) rather than defining their
own error type. That enum is already at Soroban's hard cap of 50 error
variants (`stellar-xdr`'s `ScSpecUdtErrorEnumV0` enforces this at compile
time), so a new adapter must **not** add a new variant for an adapter-specific
failure. Instead, map any new failure mode onto the closest existing
variant, following the mapping-comment convention already used for other
recently-added features in that file (e.g. the `#808`/`#804` reuse tables).

The two variants a `YieldAdapter` implementation is expected to actually
raise are:
- `SlippageExceeded` (17): `deposit` or `withdraw` returned less than the
  caller's minimum-output guard.
- `AlreadyInitialized` (1): `initialize` called on an adapter that already
  has a `Vault` key set.

Any other genuine failure inside the adapter (a malformed call to the
underlying protocol, an unexpected storage read, etc.) should propagate as
whatever the closest existing `ContractError` variant honestly describes.
Do not swallow an underlying protocol failure into a generic success.

## Fallibility and trust boundary

Adapters are the only contracts in this system that call out to code the
vault does not control. Every call an adapter makes to the underlying
protocol must be treated as fallible:

- Never assume a value-moving call to the underlying protocol succeeded
  just because it returned; check whatever the protocol's own client
  exposes to confirm the amount actually moved (see the "recompute
  position units before and after, from the pool" pattern used by both
  existing adapters — position units are derived from the difference
  between a before/after snapshot, not trusted from the protocol's return
  value alone).
- Never let a partially-failed operation leave the adapter or the vault in
  an inconsistent state. Deposits pull funds *inside* the call so a revert
  unwinds the transfer with it (see `deposit`'s value-transfer contract
  above); apply the same atomicity discipline to any new adapter's own
  interaction with its underlying protocol.

## Protocol shape mismatches are not interchangeable

A new adapter must implement `YieldAdapter` against its *own* protocol's
actual entry points. Do not assume a generic or another protocol's adapter
can be pointed at a different protocol's contract and "just work" because
the interface trait looks similar from the outside.

`adapter_blend`'s `interface_test.rs` documents exactly this failure mode as
a regression test: a generic lending adapter that calls a
`deposit(address, amount)`-shaped entry point cannot drive a real Blend pool,
whose only value-moving entry point is `submit`. The call simply fails to
resolve. When building a new adapter, write an equivalent test against a
mock of your own protocol's real interface (see
`packages/contracts/libs/test_utils/src/mocks/` for the existing
`MockBlendPool`, `MockSoroswapRouter`, and `MockSoroswapPair` as the pattern
to follow for a new protocol's mock) rather than assuming interface
conformance follows automatically from implementing the trait.

## Checklist for a new adapter

1. Create `packages/contracts/contracts/adapter_<protocol>/` with its own
   `Cargo.toml`, `src/lib.rs`, `src/test.rs`, and `src/interface_test.rs`.
2. Implement `YieldAdapter` for a new `<Protocol>AdapterContract` struct
   (see [The `YieldAdapter` trait](#the-yieldadapter-trait)).
3. Add a protocol-specific `initialize` following the shared pattern in
   [Initialization](#initialization).
4. Emit `ADAPTER` `DEPOSIT`/`WITHDRAW` events on every successful
   `deposit`/`withdraw`, matching the exact shape in [Events](#events).
5. Reuse `ContractError` variants for every failure; do not add a new
   variant (see [Errors](#errors)).
6. Add a mock of the real protocol under
   `packages/contracts/libs/test_utils/src/mocks/`, and an
   `interface_test.rs` proving a generic or mismatched adapter cannot drive
   your mock (see [Protocol shape mismatches are not interchangeable](#protocol-shape-mismatches-are-not-interchangeable)).
7. Cover slippage guards, re-initialization, and the deposit/withdraw
   atomicity contract in `src/test.rs`, following the existing
   `adapter_blend`/`adapter_soroswap` test suites as the baseline coverage
   bar.
