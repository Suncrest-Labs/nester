// ---------------------------------------------------------------------------
// Soroswap-style pool adapter integration test (issue #1352)
//
// The Blend-style lending adapter and the Soroswap-style pool adapter each
// already have thorough UNIT tests (`adapter_lending/src/test.rs`,
// `adapter_pool/src/test.rs`), exercised standalone against a mock protocol.
// What was actually missing — the real gap issue #1352 describes — is an
// INTEGRATION test: the pool adapter wired into a live vault + registry +
// strategy, moved through the vault's real `rebalance` path
// (`move_via_adapter` in `contracts/vault/src/lib.rs`), the same way
// `adapter_failure_tests.rs` exercises `MockFailingAdapter`. This proves the
// full call graph — vault -> registry -> adapter -> pool -> vault — actually
// works end to end, not just the adapter in isolation.
// ---------------------------------------------------------------------------

#[cfg(test)]
mod adapter_pool_integration {
    use nester_common::ProtocolType;
    use nester_test_utils::mocks::{MockAmmPool, MockAmmPoolClient};
    use nester_test_utils::NesterHarness;
    use soroban_sdk::{
        symbol_short, testutils::Address as _, token::StellarAssetClient, vec, Address, Symbol, Vec,
    };

    use adapter_pool::{PoolAdapterContract, PoolAdapterContractClient};
    use allocation_strategy_contract::AllocationWeight;

    const INITIAL_POOL_RESERVE: i128 = 50_000_000;

    /// A vault wired to two sources: a bookkeeping-only "cash" source (no
    /// adapter — mirrors how legacy sources work today) holding the bulk of
    /// deployed capital, and a live source backed by the real
    /// `PoolAdapterContract`, pointed at a `MockAmmPool` standing in for
    /// Soroswap. `rebalance` only ever redistributes ALREADY-deployed capital
    /// between sources (see `deployed_total` in `contracts/vault/src/lib.rs`)
    /// — it never pulls from the undeployed buffer — so a second source is
    /// what actually lets a weight change drive a real delta into the pool
    /// adapter, both directions. Mirrors
    /// `adapter_failure_tests.rs::setup_with_broken_adapter`'s wiring, with a
    /// genuine, working adapter instead of an always-reverting one.
    struct PoolFixture {
        h: NesterHarness,
        source: Symbol,
        cash: Symbol,
        adapter: PoolAdapterContractClient<'static>,
        pool: MockAmmPoolClient<'static>,
    }

    fn setup_with_pool_adapter() -> PoolFixture {
        let h = NesterHarness::setup();

        // `NesterHarness::setup()` uses `mock_all_auths()`, which records
        // auth but REJECTS any `require_auth`/authorization that isn't
        // rooted at the top-level invocation (`disable_non_root_auth: true`
        // in soroban-sdk's `switch_to_recording_auth`). The vault's real
        // adapter-pull mechanism (`authorize_adapter_pull` in
        // `contracts/vault/src/lib.rs`, called from deep inside `rebalance`)
        // grants the token transfer via `env.authorize_as_current_contract`
        // — a NON-root authorization. Under plain `mock_all_auths()` this
        // silently fails the adapter's inner `token.transfer` with a host
        // `Abort`, which `move_via_adapter`'s `try_invoke_contract` then
        // reports as a generic adapter failure — indistinguishable from a
        // genuinely broken adapter. No existing test in this repository
        // exercises a real (non-`None`, non-always-failing) adapter through
        // `rebalance` at all, so this had never surfaced before. Upgrading
        // to the non-root-auth-permitting variant here (scoped to this
        // fixture only, not the shared harness) is what actually lets this
        // integration test exercise the real path.
        h.env.mock_all_auths_allowing_non_root_auth();

        let source = symbol_short!("soroswap");
        let cash = symbol_short!("cash");

        // The mock pool needs real token liquidity backing its reserve figure
        // (see the equivalent setup in adapter_pool/src/test.rs).
        let pool_id = h.env.register_contract(None, MockAmmPool);
        let pool = MockAmmPoolClient::new(&h.env, &pool_id);
        pool.initialize(
            &h.deposit_token_id,
            &INITIAL_POOL_RESERVE,
            &INITIAL_POOL_RESERVE,
        );
        StellarAssetClient::new(&h.env, &h.deposit_token_id).mint(&pool_id, &INITIAL_POOL_RESERVE);

        let adapter_id = h.env.register_contract(None, PoolAdapterContract);
        let adapter = PoolAdapterContractClient::new(&h.env, &adapter_id);
        adapter.initialize(&h.vault_id, &pool_id, &h.deposit_token_id);

        h.registry().register_source(
            &h.admin,
            &source,
            &pool_id,
            &Some(adapter_id.clone()),
            &ProtocolType::LP,
        );
        // A bookkeeping-only source (no adapter) — a valid, pre-existing
        // pattern (`AdapterOutcome::NoAdapter` in `move_via_adapter`).
        h.registry().register_source(
            &h.admin,
            &cash,
            &Address::generate(&h.env),
            &None,
            &ProtocolType::Lending,
        );

        h.vault().set_yield_registry(&h.admin, &h.registry_id);
        h.vault().set_allocation_strategy(&h.admin, &h.strategy_id);

        // Same wiring `adapter_failure_tests.rs` documents as required: the
        // callee allowlist gates every external call rebalance makes.
        h.vault().register_callee(&h.admin, &h.strategy_id);
        h.vault().register_callee(&h.admin, &h.registry_id);
        h.vault().register_callee(&h.admin, &adapter_id);
        h.vault().register_callee(&h.admin, &h.deposit_token_id);

        // The harness's default `Balanced` strategy params (500..6_500 bps)
        // are too narrow for the swings these tests drive (up toward 6_000
        // bps on the pool source, and a `cash` weight up to 9_500 bps to
        // match) — widen them to the full available range.
        h.strategy()
            .update_strategy_params(&h.admin, &500, &10_000, &500);

        // The pool source starts at the minimum weight; almost everything
        // sits in `cash`. The deposit-direction test raises the pool
        // source's weight, which must pull capital OUT of `cash` and INTO
        // the pool adapter.
        let weights: Vec<AllocationWeight> = vec![
            &h.env,
            AllocationWeight {
                source_id: source.clone(),
                weight_bps: 500,
            },
            AllocationWeight {
                source_id: cash.clone(),
                weight_bps: 9_500,
            },
        ];
        h.strategy().set_weights(&h.admin, &weights);

        let user = h.create_user();
        h.mint_deposit_tokens(&user, 20_000_000);
        h.vault().deposit(&user, &20_000_000, &0);

        // Seed ALL starting deployed capital into `cash`, same pattern as
        // `adapter_failure_tests.rs`'s fixture — `rebalance` only
        // redistributes already-deployed capital, so the first rebalance in
        // these tests needs real deployed capital to move out of `cash` and
        // into the pool source.
        assert!(
            h.vault()
                .record_source_allocation(&h.admin, &cash, &10_000_000),
            "fixture seeding must be accepted for a freshly Active source"
        );

        PoolFixture {
            h,
            source,
            cash,
            adapter,
            pool,
        }
    }

    /// The headline acceptance criterion: a rebalance genuinely moves vault
    /// assets into the Soroswap-style pool through the adapter, and the
    /// adapter's own position accounting reflects it.
    #[test]
    fn rebalance_deposits_into_pool_adapter() {
        let f = setup_with_pool_adapter();

        assert_eq!(
            f.adapter.max_withdraw(),
            0,
            "adapter starts with no position"
        );

        let applied = f.h.vault().rebalance(&f.h.admin);

        // A source whose adapter call failed is skipped (not present in
        // `applied` at all) rather than reported with a zero delta — assert
        // it explicitly rather than letting a missing entry silently read as
        // "no movement needed".
        let entry = applied.iter().find(|d| d.source_id == f.source);
        let moved = entry
            .as_ref()
            .unwrap_or_else(|| panic!("pool source is missing from `applied` — its adapter call failed and was skipped"))
            .delta;
        assert!(
            moved > 0,
            "rebalance must deposit into the pool source, got delta {moved}"
        );

        // The adapter actually holds a position at the pool now — not just
        // vault-side bookkeeping.
        assert!(
            f.adapter.max_withdraw() > 0,
            "the pool adapter must hold real LP units after the deposit"
        );
        assert_eq!(
            f.h.vault().get_source_allocation(&f.source),
            moved,
            "vault bookkeeping must match what actually moved through the adapter"
        );
    }

    /// A subsequent withdraw-direction rebalance (driven by a weight cut)
    /// pulls assets back out of the pool through the same adapter, proving
    /// the round trip, not just the one-way deposit.
    #[test]
    fn rebalance_withdraws_from_pool_adapter_on_reduced_weight() {
        let f = setup_with_pool_adapter();

        // First raise the pool source's target weight and rebalance, so it
        // holds a real, sizeable position to withdraw from.
        let raised: Vec<AllocationWeight> = vec![
            &f.h.env,
            AllocationWeight {
                source_id: f.source.clone(),
                weight_bps: 6_000,
            },
            AllocationWeight {
                source_id: f.cash.clone(),
                weight_bps: 4_000,
            },
        ];
        f.h.strategy().set_weights(&f.h.admin, &raised);
        f.h.vault().rebalance(&f.h.admin);
        let allocated = f.h.vault().get_source_allocation(&f.source);
        assert!(allocated > 0, "fixture must have deposited first");

        // Now cut it back down to the harness's minimum weight so the next
        // rebalance pulls most of that capital back out of the pool adapter.
        let weights: Vec<AllocationWeight> = vec![
            &f.h.env,
            AllocationWeight {
                source_id: f.source.clone(),
                weight_bps: 500,
            },
            AllocationWeight {
                source_id: f.cash.clone(),
                weight_bps: 9_500,
            },
        ];
        f.h.strategy().set_weights(&f.h.admin, &weights);

        let before_units = f.adapter.max_withdraw();
        f.h.vault().rebalance(&f.h.admin);
        let after_units = f.adapter.max_withdraw();

        assert!(
            after_units < before_units,
            "reducing target weight must withdraw LP units from the adapter \
             (before {before_units}, after {after_units})"
        );
        assert!(
            f.h.vault().get_source_allocation(&f.source) < allocated,
            "vault-side allocation bookkeeping must shrink to match"
        );
    }

    /// The pool's reserves growing (simulated AMM fee accrual) must be
    /// reflected back through the adapter's own valuation, proving the
    /// adapter's pro-rata pricing genuinely reaches live pool state and is
    /// not just returning a cached figure.
    #[test]
    fn adapter_position_value_tracks_pool_reserve_growth() {
        let f = setup_with_pool_adapter();
        f.h.vault().rebalance(&f.h.admin);
        assert!(
            f.adapter.max_withdraw() > 0,
            "fixture must have deposited first"
        );

        let before = f.adapter.position_value(&f.h.vault_id);
        f.pool.simulate_fee_growth(&(INITIAL_POOL_RESERVE * 2));
        let after = f.adapter.position_value(&f.h.vault_id);

        assert!(
            after > before,
            "adapter position value must grow with real pool reserve growth \
             (before {before}, after {after})"
        );
    }
}
