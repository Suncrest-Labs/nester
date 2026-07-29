#[cfg(test)]
mod test {
    use crate::{RecurringDepositContract, RecurringDepositContractClient};
    use nester_common::ContractError as Error;
    use nester_test_utils::harness::NesterHarness;
    use soroban_sdk::{
        testutils::Address as _, testutils::Ledger as _, Address, ConversionError, Env,
        InvokeError,
    };

    fn setup_contract() -> (Env, Address, RecurringDepositContractClient<'static>) {
        let env = Env::default();
        env.ledger().set_timestamp(1000);
        env.mock_all_auths_allowing_non_root_auth();
        let contract_id = env.register_contract(None, RecurringDepositContract);
        let client = RecurringDepositContractClient::new(&env, &contract_id);
        let admin = Address::generate(&env);

        client.initialize(&admin);

        (env, contract_id, client)
    }

    fn setup_with_harness() -> (
        NesterHarness,
        Address,
        RecurringDepositContractClient<'static>,
    ) {
        let harness = NesterHarness::setup();
        harness.env.ledger().set_timestamp(1000);
        harness.env.mock_all_auths_allowing_non_root_auth();
        let contract_id = harness
            .env
            .register_contract(None, RecurringDepositContract);
        let client = RecurringDepositContractClient::new(&harness.env, &contract_id);

        let admin = Address::generate(&harness.env);
        client.initialize(&admin);

        (harness, contract_id, client)
    }

    fn advance_time(env: &Env, seconds: u64) {
        env.ledger().set_timestamp(env.ledger().timestamp() + seconds);
    }

    fn contract_err_u64(
        err: Error,
    ) -> Result<Result<u64, soroban_sdk::Error>, Result<soroban_sdk::Error, InvokeError>> {
        Err(Ok(soroban_sdk::Error::from_contract_error(err as u32)))
    }

    fn contract_err_void(
        err: Error,
    ) -> Result<Result<(), soroban_sdk::ConversionError>, Result<soroban_sdk::Error, InvokeError>> {
        Err(Ok(soroban_sdk::Error::from_contract_error(err as u32)))
    }

    #[test]
    fn test_create_mandate() {
        let (env, _contract_id, client) = setup_contract();
        let user = Address::generate(&env);
        let vault = Address::generate(&env);
        let token = Address::generate(&env);

        let mandate_id = client.create_mandate(
            &user,
            &vault,
            &token,
            &10000i128,                                    // amount_per_period
            &(7 * 24 * 3600u64),                           // weekly (period_secs)
            &env.ledger().timestamp(),                    // start_at
            &(env.ledger().timestamp() + 365 * 24 * 3600), // expires_at (1 year)
            &(52 * 10000i128),                             // max_total (52 weeks worth)
        );

        assert_eq!(mandate_id, 1);

        let mandate = client.get_mandate(&mandate_id);
        assert_eq!(mandate.user, user);
        assert_eq!(mandate.vault, vault);
        assert_eq!(mandate.amount_per_period, 10000);
        assert_eq!(mandate.period_secs, 7 * 24 * 3600);
        assert_eq!(mandate.total_drawn, 0);
        assert!(mandate.is_active);
        assert!(!mandate.is_paused);
    }

    #[test]
    fn test_execute_mandate_timing() {
        let (harness, _contract_id, client) = setup_with_harness();
        let env = &harness.env;

        let user = harness.create_user();
        let vault = harness.vault_id.clone();
        let token = harness.deposit_token_id.clone();

        harness.mint_deposit_tokens(&user, 100000);

        let start_time = env.ledger().timestamp() + 3600; // 1 hour from now
        let mandate_id = client.create_mandate(
            &user,
            &vault,
            &token,
            &10000i128,
            &3600u64, // 1 hour period
            &start_time,
            &(start_time + 10 * 3600), // 10 hours total
            &100000i128,
        );

        let caller = Address::generate(env);

        // Should fail before start time
        let result = client.try_execute_mandate(&caller, &user, &mandate_id);
        assert_eq!(result, contract_err_void(Error::TimelockNotReady));

        // Advance to start time
        env.ledger().set_timestamp(start_time);

        // Should work at start time
        client.execute_mandate(&caller, &user, &mandate_id);

        let mandate = client.get_mandate(&mandate_id);
        assert_eq!(mandate.total_drawn, 10000);
        assert_eq!(mandate.last_executed_at, start_time);

        // Should not work immediately after
        let result = client.try_execute_mandate(&caller, &user, &mandate_id);
        assert_eq!(result, contract_err_void(Error::TimelockNotReady));

        // Advance time by one period
        advance_time(env, 3600);

        // Should work after period passes
        client.execute_mandate(&caller, &user, &mandate_id);

        let mandate = client.get_mandate(&mandate_id);
        assert_eq!(mandate.total_drawn, 20000);
        assert_eq!(mandate.last_executed_at, start_time + 3600);
    }

    #[test]
    fn test_catch_up_semantics() {
        let (harness, _contract_id, client) = setup_with_harness();
        let env = &harness.env;

        let user = harness.create_user();
        let vault = harness.vault_id.clone();
        let token = harness.deposit_token_id.clone();

        harness.mint_deposit_tokens(&user, 100000);

        let start_time = env.ledger().timestamp();
        let mandate_id = client.create_mandate(
            &user,
            &vault,
            &token,
            &10000i128,
            &3600u64, // 1 hour period
            &start_time,
            &(start_time + 10 * 3600),
            &100000i128,
        );

        let caller = Address::generate(env);

        // Execute first time
        client.execute_mandate(&caller, &user, &mandate_id);

        // Skip 3 periods (3 hours)
        advance_time(env, 3 * 3600);

        // Execute second time - should only advance by one period
        client.execute_mandate(&caller, &user, &mandate_id);

        let mandate = client.get_mandate(&mandate_id);
        assert_eq!(mandate.last_executed_at, start_time + 3600); // Only one period advanced

        // Can execute again after one more period
        advance_time(env, 3600);
        client.execute_mandate(&caller, &user, &mandate_id);

        let mandate = client.get_mandate(&mandate_id);
        assert_eq!(mandate.last_executed_at, start_time + 2 * 3600); // Another period advanced
    }

    #[test]
    fn test_mandate_pause_resume() {
        let (harness, _contract_id, client) = setup_with_harness();
        let env = &harness.env;

        let user = harness.create_user();
        let vault = harness.vault_id.clone();
        let token = harness.deposit_token_id.clone();

        harness.mint_deposit_tokens(&user, 100000);

        let start_time = env.ledger().timestamp();
        let mandate_id = client.create_mandate(
            &user,
            &vault,
            &token,
            &10000i128,
            &3600u64,
            &start_time,
            &(start_time + 10 * 3600),
            &100000i128,
        );

        let caller = Address::generate(env);

        // Pause mandate
        client.pause_mandate(&user, &mandate_id, &true);

        let mandate = client.get_mandate(&mandate_id);
        assert!(mandate.is_paused);

        // Should fail when paused
        let result = client.try_execute_mandate(&caller, &user, &mandate_id);
        assert_eq!(result, contract_err_void(Error::InvalidOperation));

        // Resume mandate
        client.resume_mandate(&user, &mandate_id);

        let mandate = client.get_mandate(&mandate_id);
        assert!(!mandate.is_paused);

        // Should work when resumed
        client.execute_mandate(&caller, &user, &mandate_id);
    }

    #[test]
    fn test_mandate_cancel() {
        let (harness, _contract_id, client) = setup_with_harness();
        let env = &harness.env;

        let user = harness.create_user();
        let vault = harness.vault_id.clone();
        let token = harness.deposit_token_id.clone();

        let start_time = env.ledger().timestamp();
        let mandate_id = client.create_mandate(
            &user,
            &vault,
            &token,
            &10000i128,
            &3600u64,
            &start_time,
            &(start_time + 10 * 3600),
            &100000i128,
        );

        let caller = Address::generate(env);

        // Cancel mandate
        client.cancel_mandate(&user, &mandate_id);

        let mandate = client.get_mandate(&mandate_id);
        assert!(!mandate.is_active);

        // Should fail when cancelled
        let result = client.try_execute_mandate(&caller, &user, &mandate_id);
        assert_eq!(result, contract_err_void(Error::InvalidOperation));
    }

    #[test]
    fn test_mandate_limits() {
        let (harness, _contract_id, client) = setup_with_harness();
        let env = &harness.env;

        let user = harness.create_user();
        let vault = harness.vault_id.clone();
        let token = harness.deposit_token_id.clone();

        harness.mint_deposit_tokens(&user, 50000); // Less than max_total

        let start_time = env.ledger().timestamp();
        let mandate_id = client.create_mandate(
            &user,
            &vault,
            &token,
            &30000i128, // Large amount per period
            &3600u64,
            &start_time,
            &(start_time + 10 * 3600),
            &50000i128, // max_total
        );

        let caller = Address::generate(env);

        // First execution should work
        client.execute_mandate(&caller, &user, &mandate_id);

        let mandate = client.get_mandate(&mandate_id);
        assert_eq!(mandate.total_drawn, 30000);

        // Second execution should fail due to max_total limit
        advance_time(env, 3600);
        let result = client.try_execute_mandate(&caller, &user, &mandate_id);
        assert_eq!(result, contract_err_void(Error::BudgetExhausted));
    }

    #[test]
    fn test_insufficient_balance() {
        let (harness, _contract_id, client) = setup_with_harness();
        let env = &harness.env;

        let user = harness.create_user();
        let vault = harness.vault_id.clone();
        let token = harness.deposit_token_id.clone();

        // Don't fund the user - should have insufficient balance

        let start_time = env.ledger().timestamp();
        let mandate_id = client.create_mandate(
            &user,
            &vault,
            &token,
            &10000i128,
            &3600u64,
            &start_time,
            &(start_time + 10 * 3600),
            &100000i128,
        );

        let caller = Address::generate(env);

        // Should fail due to insufficient balance but not deactivate mandate
        let result = client.try_execute_mandate(&caller, &user, &mandate_id);
        assert!(result.is_err()); // InsufficientBalance from token contract

        let mandate = client.get_mandate(&mandate_id);
        assert!(mandate.is_active); // Mandate should still be active

        // Fund user and try again
        harness.mint_deposit_tokens(&user, 100000);

        // Should work now
        client.execute_mandate(&caller, &user, &mandate_id);

        let mandate = client.get_mandate(&mandate_id);
        assert_eq!(mandate.total_drawn, 10000);
    }

    #[test]
    fn test_next_execution_at() {
        let (env, _contract_id, client) = setup_contract();
        let user = Address::generate(&env);
        let vault = Address::generate(&env);
        let token = Address::generate(&env);

        let start_time = env.ledger().timestamp() + 3600; // 1 hour from now
        let mandate_id = client.create_mandate(
            &user,
            &vault,
            &token,
            &10000i128,
            &3600u64,
            &start_time,
            &(start_time + 10 * 3600),
            &100000i128,
        );

        // Before start time, should return start_time
        let next = client.next_execution_at(&user, &mandate_id);
        assert_eq!(next, start_time);

        // Pause mandate - should return 0
        client.pause_mandate(&user, &mandate_id, &true);
        let next = client.next_execution_at(&user, &mandate_id);
        assert_eq!(next, 0);

        // Resume and advance to start time
        client.resume_mandate(&user, &mandate_id);
        env.ledger().set_timestamp(start_time);

        let next = client.next_execution_at(&user, &mandate_id);
        assert_eq!(next, start_time);
    }

    #[test]
    fn test_user_mandates_list() {
        let (env, _contract_id, client) = setup_contract();
        let user = Address::generate(&env);
        let vault = Address::generate(&env);
        let token = Address::generate(&env);

        let start_time = env.ledger().timestamp();

        // Create multiple mandates
        let mandate1 = client.create_mandate(
            &user,
            &vault,
            &token,
            &10000i128,
            &3600u64,
            &start_time,
            &(start_time + 3600),
            &50000i128,
        );

        let mandate2 = client.create_mandate(
            &user,
            &vault,
            &token,
            &5000i128,
            &7200u64,
            &start_time,
            &(start_time + 7200),
            &25000i128,
        );

        let user_mandates = client.get_user_mandates(&user);
        assert_eq!(user_mandates.len(), 2);
        assert!(user_mandates.contains(&mandate1));
        assert!(user_mandates.contains(&mandate2));

        // Cancel one mandate
        client.cancel_mandate(&user, &mandate1);

        let user_mandates = client.get_user_mandates(&user);
        assert_eq!(user_mandates.len(), 1);
        assert_eq!(user_mandates.get(0).unwrap(), mandate2);
    }

    #[test]
    fn test_input_validation() {
        let (env, _contract_id, client) = setup_contract();
        let user = Address::generate(&env);
        let vault = Address::generate(&env);
        let token = Address::generate(&env);

        let start_time = env.ledger().timestamp();

        // Test invalid amount
        let result = client.try_create_mandate(
            &user,
            &vault,
            &token,
            &0i128,
            &3600u64,
            &start_time,
            &(start_time + 3600),
            &50000i128,
        );
        assert_eq!(result, contract_err_u64(Error::InvalidAmount));

        // Test invalid period (too short)
        let result = client.try_create_mandate(
            &user,
            &vault,
            &token,
            &10000i128,
            &1800u64, // 30 minutes
            &start_time,
            &(start_time + 3600),
            &50000i128,
        );
        assert_eq!(result, contract_err_u64(Error::InvalidAmount));

        // Test invalid expiry (before start)
        let result = client.try_create_mandate(
            &user,
            &vault,
            &token,
            &10000i128,
            &3600u64,
            &start_time,
            &(start_time - 1),
            &50000i128,
        );
        assert_eq!(result, contract_err_u64(Error::InvalidAmount));

        // Test invalid max_total (less than amount_per_period)
        let result = client.try_create_mandate(
            &user,
            &vault,
            &token,
            &10000i128,
            &3600u64,
            &start_time,
            &(start_time + 3600),
            &5000i128,
        );
        assert_eq!(result, contract_err_u64(Error::InvalidAmount));
    }
}