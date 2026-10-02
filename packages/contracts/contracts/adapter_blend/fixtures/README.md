# Blend Capital fixtures

Official, unmodified Blend Capital contract WASM, vendored so
`real_pool_test.rs` exercises `adapter_blend` against Blend's real interface
instead of a hand-written mock. All are open source (Apache-2.0/MIT, see
each source repository) and pinned to v1.0.0 because that release matches
this workspace's `soroban-sdk = "=21.7.7"` / protocol 21 host. Blend's
v2.0.0 pool WASM targets protocol 22 and fails `register_contract_wasm` here
with `"contract protocol number is newer than host"` -- confirmed directly,
not assumed.

| File | Source | SHA-256 |
|---|---|---|
| `blend_pool.wasm` | [blend-capital/blend-contracts v1.0.0](https://github.com/blend-capital/blend-contracts/releases/tag/v1.0.0_pool_v1.0.0.wasm) | `baf978f10efdbcd85747868bef8832845ea6809f7643b67a4ac0cd669327fc2c` |
| `blend_pool_factory.wasm` | [blend-capital/blend-contracts v1.0.0](https://github.com/blend-capital/blend-contracts/releases/tag/v1.0.0_pool-factory_v1.0.0.wasm) | `0287f4ad7350935b83d94e046c0bcabc960b233dbce1531008c021b71d406a1d` |
| `blend_backstop.wasm` | [blend-capital/blend-contracts v1.0.0](https://github.com/blend-capital/blend-contracts/releases/tag/v1.0.0_backstop_v1.0.0.wasm) | `62f61b32fff99f7eec052a8e573c367759f161c481a5caf0e76a10ae4617c3b4` |
| `blend_emitter.wasm` | [blend-capital/blend-contracts v1.0.0](https://github.com/blend-capital/blend-contracts/releases/tag/v1.0.0_emitter_v1.0.0.wasm) | `438a5528cff17ede6fe515f095c43c5f15727af17d006971485e52462e7e7b89` |
| `blend_comet.wasm` | [blend-capital/blend-utils `wasm_v1/comet.wasm`](https://github.com/blend-capital/blend-utils/blob/main/wasm_v1/comet.wasm) | `8abc28913035c07411ed5d134e6bfeab4723d97ddd4d1a22a0605d35c94d1a36` |

The Comet AMM (Blend's backstop LP token) has no tagged GitHub release, so
its WASM comes from `blend-utils`'s own deployment fixture directory instead
-- the same binary Blend's own team uses to bootstrap local/testnet
deployments (see `real_pool_test.rs`'s `setup()`, which follows
`blend-utils`'s `deployBlend` / `setupPoolBackstop` reference scripts for
initialization order and bootstrap amounts).

To refresh a pinned version: download the new release asset, confirm
`stellar contract info meta --wasm <file>` still reports an `rssdkver`
compatible with this workspace's `soroban-sdk`, update the checksum above,
and re-run `cargo test -p adapter-blend-contract real_pool_test` to confirm
the deployment/activation sequence in `setup()` still matches the new
version's interface.
