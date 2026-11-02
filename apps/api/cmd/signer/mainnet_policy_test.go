package main

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/signing"
)

// loadMainnetFixture reads the mainnet-style signer configuration fixture and
// installs every key as an environment variable for the duration of the test, so
// loadConfig exercises the same parsing path a production signer uses.
func loadMainnetFixture(t *testing.T) {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "mainnet-signer.env"))
	if err != nil {
		t.Fatalf("open mainnet fixture: %v", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		t.Setenv(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan mainnet fixture: %v", err)
	}
}

func assertPolicyRejected(t *testing.T, err error, want signing.Rejection) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected rejection %q, got nil (the intent would have been SIGNED)", want)
	}
	if !errors.Is(err, signing.ErrPolicyRejected) {
		t.Fatalf("expected a policy rejection, got %v", err)
	}
	var pe *signing.PolicyError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *signing.PolicyError, got %T", err)
	}
	if pe.Category != want {
		t.Fatalf("expected rejection category %q, got %q (reason: %s)", want, pe.Category, pe.Reason)
	}
}

// TestMainnetPolicyFixture loads the signer policy from a mainnet-style config
// fixture and exercises the five cases the issue calls out: an allowed deposit
// at the configured limit, a deposit one stroop over it, a non-allowlisted
// contract, a testnet-passphrase intent against a mainnet signer, and a stale
// intent. Each case is asserted against its expected rejection category.
func TestMainnetPolicyFixture(t *testing.T) {
	loadMainnetFixture(t)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig from mainnet fixture: %v", err)
	}

	policy := signing.NewPolicy(
		cfg.networkPassphrase,
		cfg.allowedContracts,
		cfg.allowedOperations,
		cfg.maxAmountStroops,
		cfg.maxIntentAge,
		cfg.clockSkew,
	)

	if len(cfg.allowedContracts) == 0 {
		t.Fatal("fixture must allow at least one contract")
	}
	mainnetContract := cfg.allowedContracts[0]
	now := time.Now().UTC()

	// 1. Allowed deposit exactly at the configured limit.
	allowed := &signing.Intent{
		ID:                "intent-mainnet-allowed",
		Operation:         signing.OpDeposit,
		Shape:             signing.ShapeI128Pair,
		ContractAddress:   mainnetContract,
		NetworkPassphrase: cfg.networkPassphrase,
		Arg0:              cfg.maxAmountStroops,
		Arg1:              0,
		IssuedAt:          now,
	}
	if err := allowed.Validate(); err != nil {
		t.Fatalf("allowed deposit should pass structural validation: %v", err)
	}
	if err := policy.Evaluate(allowed, now); err != nil {
		t.Fatalf("allowed deposit at the limit should be accepted, got: %v", err)
	}

	// 2. Deposit one stroop over the configured limit.
	over := *allowed
	over.ID = "intent-mainnet-over"
	over.Arg0 = cfg.maxAmountStroops + 1
	if err := over.Validate(); err != nil {
		t.Fatalf("over-limit deposit should be structurally valid: %v", err)
	}
	assertPolicyRejected(t, policy.Evaluate(&over, now), signing.RejectAmountOutOfPolicy)

	// 3. Non-allowlisted contract.
	other := *allowed
	other.ID = "intent-mainnet-other-contract"
	other.ContractAddress = "CB" + strings.Repeat("A", 54)
	if err := other.Validate(); err != nil {
		t.Fatalf("other-contract deposit should be structurally valid: %v", err)
	}
	assertPolicyRejected(t, policy.Evaluate(&other, now), signing.RejectContractNotAllowed)

	// 4. Testnet-passphrase intent against a mainnet signer.
	testnet := *allowed
	testnet.ID = "intent-mainnet-testnet"
	testnet.NetworkPassphrase = "Test SDF Network ; September 2015"
	if err := testnet.Validate(); err != nil {
		t.Fatalf("testnet-passphrase intent should be structurally valid: %v", err)
	}
	assertPolicyRejected(t, policy.Evaluate(&testnet, now), signing.RejectNetworkMismatch)

	// 5. Stale intent issued well beyond MaxIntentAge.
	stale := *allowed
	stale.ID = "intent-mainnet-stale"
	stale.IssuedAt = now.Add(-10 * time.Minute)
	if err := stale.Validate(); err != nil {
		t.Fatalf("stale intent should be structurally valid: %v", err)
	}
	assertPolicyRejected(t, policy.Evaluate(&stale, now), signing.RejectIntentExpired)
}
