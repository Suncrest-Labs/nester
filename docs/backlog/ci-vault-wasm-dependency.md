# [SC-13] Ensure cargo test for vault contracts passes with vault_token.wasm present

**Status:** Open  
**Priority:** Medium  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Testing/CI

## Issue

Integration tests in the vault contract may depend on the compiled `vault_token.wasm` artifact. If the WASM binary is not built before running `cargo test`, tests may silently skip or fail to exercise the intended code paths, yielding a false green CI result.

**Related PRD claims:**
- [SC-13] Confirm `cargo test -p vault-contract` passes in CI with vault_token.wasm present
- [P-10] Cargo test in CI may pass vacuously without vault_token.wasm artifact

## Acceptance Criteria

- [ ] Add explicit dependency in `.github/workflows/ci.yml` to build `vault_token.wasm` before running `cargo test -p vault-contract`
- [ ] Verify that all contract tests run and do not skip (check for `test result:` output)
- [ ] Document in `.github/workflows/ci.yml` comments why vault_token.wasm must be present
- [ ] Test the CI workflow locally by temporarily removing WASM artifact and verifying tests fail or skip clearly
- [ ] Ensure workflow logs show WASM artifact build step and confirm artifact exists before cargo test

## Implementation Notes

- Add a build step in CI before test: `cargo build --package vault-token-contract --target wasm32-unknown-unknown --release`
- Verify artifact path matches what tests expect (typically `target/wasm32-unknown-unknown/release/vault_token.wasm`)
- Consider caching WASM artifacts between workflow runs to reduce build time
- Add a sanity check: `[ -f <wasm_path> ] || exit 1` before running tests

## Evidence References

Once resolved:
- `file: .github/workflows/ci.yml#<lines>` (dependency and artifact check)
- `ci: ci.yml::rust-contracts` (verified job logs)

## Related Issues

- PRD [P-10]
- GitHub issue #1115
