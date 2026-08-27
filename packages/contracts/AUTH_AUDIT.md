# require_auth audit (Issue #1132)

## Finding

The audit enumerated every `pub fn` taking a caller-shaped `Address` parameter
and checked whether `require_auth` appears in its body. The `propose_upgrade`
and `cancel_upgrade` entrypoints on `allocation_strategy`, `treasury`,
`yield_registry`, and `vault` were flagged, because each calls
`AccessControl::require_role(&env, &admin, Role::Upgrader)` without a directly
adjacent `admin.require_auth()`.

**These eight functions are not vulnerable.** Each delegates to
`nester_common::Upgrade::propose_upgrade` / `Upgrade::cancel_upgrade`, and both
of those library functions call `proposer.require_auth()` / `canceller.require_auth()`
as their first statement. The signature check is enforced — it just lives one
level down, which is why a body-level grep does not see it. The library's own
doc comments already state this contract explicitly:

> `proposer` must authorize the invocation. The calling contract entry point is
> responsible for verifying that `proposer` holds `Role::Upgrader`.

So the split is deliberate: the library owns the auth check, the entrypoint owns
the role check. This matches the other shared-library cases the audit resolved
as false positives (`AccessControl::initialize`, `Timelock::propose`, etc.).

Adding `admin.require_auth()` to the entrypoint on top of the library's call
is not a hardening no-op — it is a bug. Soroban rejects a second `require_auth`
for the same address in an already-authorized frame with
`Error(Auth, ExistingValue)` ("frame is already authorized"), which breaks the
entrypoint for the legitimate role holder. Four integration tests in
`tests/integration/src/integration/lifecycle_tests.rs` fail on exactly this:

- `test_upgrade_lifecycle_full_flow`
- `test_upgrade_cancellation_and_access_control`
- `test_emergency_withdrawal_during_pending_upgrade`
- `test_treasury_upgrade_delay_requirement`

`execute_upgrade` on these same contracts is intentionally permissionless once
a proposal matures — documented as such in each contract, and a deliberate
design choice rather than a gap.

## Outcome

No production code change was required. The audit's value is the verification
itself plus regression coverage that pins the behavior in place.

## Regression coverage

Extended `privileged_strategy_calls_require_signatures` in `allocation_strategy`
(which clears all mocked auths and asserts every privileged call fails) to also
cover `propose_upgrade`/`cancel_upgrade`. Because their auth check is inherited
from a shared library rather than written in the entrypoint, it is the kind of
guarantee a future refactor could silently drop; this test fails if that happens.

## Method

Automated enumeration of `pub fn`s with a caller-shaped `Address` parameter,
checking for `require_auth` in the body (resolving one level of
`Self::..._internal` delegation), then manual verification of each candidate.
The lesson from this pass is that one level of delegation is not enough —
candidates must also be resolved through shared-library calls
(`nester_common::Upgrade`, `AccessControl`, `Timelock`) before being reported.

## Scope not covered by this pass

Not a claim that every entrypoint in every contract has been individually
enumerated with its intended authorization documented, nor does it add a CI
check that fails when a new entrypoint lacks a matching auth test. A CI gate
would be the durable version of this audit — it is real follow-up work.
