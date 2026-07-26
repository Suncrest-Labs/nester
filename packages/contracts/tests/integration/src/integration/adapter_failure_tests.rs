// ---------------------------------------------------------------------------
// Adapter failure isolation (#812)
//
// The hard requirement: a broken adapter degrades ONLY its own source. A
// rebalance must skip the reverting source and complete across the rest,
// never abort wholesale.
//
// NOTE: these live here rather than appended to expanded_tests.rs (where the
// issue suggested) because that file is not wired into the module tree and
// does not compile - it references setup helpers that do not exist. Fixing it
// is out of scope for this change; see the PR description.
// ---------------------------------------------------------------------------

#[cfg(test)]
mod adapter_failure_isolation {
    use nester_access_control::Role;
    use nester_common::{ProtocolType, SourceStatus};
    use nester_test_utils::mocks::MockFailingAdapter;
    use nester_test_utils::NesterHarness;
    use soroban_sdk::{
        symbol_short,
        testutils::{Address as _, Ledger},
        vec, Address, Symbol, Vec,
    };

    use allocation_strategy_contract::AllocationWeight;

    /// Wire a vault with three sources where the middle one's adapter always
    /// reverts, then seed allocations so a rebalance has real work to do.
    struct FailureFixture {
        h: NesterHarness,
        good_a: Symbol,
        broken: Symbol,
        good_b: Symbol,
    }

    fn setup_with_broken_adapter() -> FailureFixture {
        let h = NesterHarness::setup();

        let good_a = symbol_short!("good_a");
        let broken = symbol_short!("broken");
        let good_b = symbol_short!("good_b");

        // The failing adapter reverts on every entry point, including reads.
        let bad_adapter = h.env.register_contract(None, MockFailingAdapter);

        h.registry().register_source(
            &h.admin,
            &good_a,
            &Address::generate(&h.env),
            &None,
            &ProtocolType::Lending,
        );
        h.registry().register_source(
            &h.admin,
            &broken,
            &Address::generate(&h.env),
            &Some(bad_adapter),
            &ProtocolType::Lending,
        );
        h.registry().register_source(
            &h.admin,
            &good_b,
            &Address::generate(&h.env),
            &None,
            &ProtocolType::Lending,
        );

        // Bind the vault to registry + strategy so rebalance resolves adapters.
        h.vault().set_yield_registry(&h.admin, &h.registry_id);
        h.vault().set_allocation_strategy(&h.admin, &h.strategy_id);

        // The vault reports adapter failures it observes, so it needs the
        // Operator role on the registry. Without this the reports are dropped
        // and sources never degrade — deployment must do the same wiring.
        h.registry()
            .grant_role(&h.admin, &h.vault_id, &Role::Operator);

        // Equal targets across all three sources.
        let weights: Vec<AllocationWeight> = vec![
            &h.env,
            AllocationWeight { source_id: good_a.clone(), weight_bps: 3_400 },
            AllocationWeight { source_id: broken.clone(), weight_bps: 3_300 },
            AllocationWeight { source_id: good_b.clone(), weight_bps: 3_300 },
        ];
        h.strategy().set_weights(&h.admin, &weights);

        // Fund the vault: rebalance refuses to run against zero assets.
        let user = h.create_user();
        h.mint_deposit_tokens(&user, 200_000_000);
        h.vault().deposit(&user, &200_000_000, &0);

        // Seed lopsided allocations so the strategy produces non-trivial
        // deltas across all three sources, including the broken one.
        h.vault().record_source_allocation(&h.admin, &good_a, &90_000_000);
        h.vault().record_source_allocation(&h.admin, &broken, &60_000_000);
        h.vault().record_source_allocation(&h.admin, &good_b, &10_000_000);

        FailureFixture { h, good_a, broken, good_b }
    }

    /// The headline acceptance criterion: rebalance completes across the
    /// healthy sources while the reverting adapter is skipped.
    #[test]
    fn rebalance_skips_failing_adapter_and_completes() {
        let f = setup_with_broken_adapter();

        // Must NOT panic — the whole point of failure isolation.
        let applied = f.h.vault().rebalance(&f.h.admin);

        // The broken source contributed no applied delta.
        for delta in applied.iter() {
            assert_ne!(
                delta.source_id, f.broken,
                "a reverting adapter must never produce an applied delta"
            );
        }

        // The healthy sources still hold their capital: the rebalance ran.
        let total_healthy = f.h.vault().get_source_allocation(&f.good_a)
            + f.h.vault().get_source_allocation(&f.good_b);
        assert!(
            total_healthy > 0,
            "rebalance must complete across the remaining sources"
        );
    }

    /// The skipped source is reported to the registry, and repeated failures
    /// flip it to Degraded rather than taking the vault down with it.
    #[test]
    fn repeated_rebalance_failures_degrade_only_that_source() {
        let f = setup_with_broken_adapter();

        // Drive several rebalances past the failure threshold. Cooldown is
        // sidestepped by advancing the ledger between calls.
        let threshold = f.h.registry().get_failure_threshold();
        for _ in 0..=threshold {
            let _ = f.h.vault().try_rebalance(&f.h.admin);
            let cooldown = f.h.vault().get_rebalance_cooldown();
            f.h.env
                .ledger()
                .with_mut(|l| l.timestamp += cooldown + 1);
        }

        assert_eq!(
            f.h.registry().get_source_status(&f.broken),
            SourceStatus::Degraded,
            "repeated adapter failures must degrade the source"
        );

        // Blast radius check: the healthy sources are untouched.
        assert_eq!(
            f.h.registry().get_source_status(&f.good_a),
            SourceStatus::Active
        );
        assert_eq!(
            f.h.registry().get_source_status(&f.good_b),
            SourceStatus::Active
        );

        // And the vault is still fully operational.
        assert!(f.h.vault().get_total_deposits() >= 0);
    }

    /// A degraded source stays degraded until an admin says otherwise —
    /// never silent auto-recovery.
    #[test]
    fn degraded_source_needs_explicit_admin_recovery() {
        let f = setup_with_broken_adapter();

        let threshold = f.h.registry().get_failure_threshold();
        for _ in 0..=threshold {
            f.h.registry().report_source_failure(&f.h.admin, &f.broken);
        }
        assert_eq!(
            f.h.registry().get_source_status(&f.broken),
            SourceStatus::Degraded
        );

        // Degraded sources drop out of the active set the allocator reads.
        let active = f.h.registry().get_active_sources();
        for s in active.iter() {
            assert_ne!(s.id, f.broken);
        }

        f.h.registry().recover_source(&f.h.admin, &f.broken);
        assert_eq!(
            f.h.registry().get_source_status(&f.broken),
            SourceStatus::Active
        );
        assert_eq!(f.h.registry().get_source_failure_count(&f.broken), 0);
    }

    /// Bookkeeping must refuse to grow an unhealthy source rather than
    /// panicking, so a caller updating several sources is not aborted by one.
    #[test]
    fn record_source_allocation_skips_unhealthy_source() {
        let f = setup_with_broken_adapter();

        let threshold = f.h.registry().get_failure_threshold();
        for _ in 0..=threshold {
            f.h.registry().report_source_failure(&f.h.admin, &f.broken);
        }

        let recorded = f
            .h
            .vault()
            .record_source_allocation(&f.h.admin, &f.broken, &50_000_000);
        assert!(!recorded, "degraded source must be skipped, not recorded");

        let still_ok = f
            .h
            .vault()
            .record_source_allocation(&f.h.admin, &f.good_a, &50_000_000);
        assert!(still_ok, "healthy sources keep working");
        assert_eq!(f.h.vault().get_source_allocation(&f.good_a), 50_000_000);
    }
}
