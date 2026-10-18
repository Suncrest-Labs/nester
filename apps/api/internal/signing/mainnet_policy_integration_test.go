package signing

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func loadEnvFixture(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture %s: %v", path, err)
	}
	defer f.Close()

	env := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			t.Fatalf("malformed fixture line: %q", line)
		}
		env[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	return env
}

func buildPolicyFromMainnetFixture(t *testing.T, env map[string]string) *Policy {
	t.Helper()
	passphrase := env["STELLAR_NETWORK_PASSPHRASE"]
	if passphrase == "" {
		t.Fatal("STELLAR_NETWORK_PASSPHRASE missing from fixture")
	}

	var contracts []string
	for _, c := range strings.Split(env["SIGNER_ALLOWED_CONTRACTS"], ",") {
		if trimmed := strings.TrimSpace(c); trimmed != "" {
			contracts = append(contracts, trimmed)
		}
	}

	var ops []Operation
	for _, rawOp := range strings.Split(env["SIGNER_ALLOWED_OPERATIONS"], ",") {
		if trimmed := strings.TrimSpace(rawOp); trimmed != "" {
			ops = append(ops, Operation(trimmed))
		}
	}

	maxStroops, err := strconv.ParseInt(env["SIGNER_MAX_AMOUNT_STROOPS"], 10, 64)
	if err != nil {
		t.Fatalf("parse SIGNER_MAX_AMOUNT_STROOPS: %v", err)
	}
	maxAge, err := time.ParseDuration(env["SIGNER_MAX_INTENT_AGE"])
	if err != nil {
		t.Fatalf("parse SIGNER_MAX_INTENT_AGE: %v", err)
	}
	clockSkew, err := time.ParseDuration(env["SIGNER_CLOCK_SKEW"])
	if err != nil {
		t.Fatalf("parse SIGNER_CLOCK_SKEW: %v", err)
	}

	return NewPolicy(passphrase, contracts, ops, maxStroops, maxAge, clockSkew)
}

// TestMainnetPolicyIntegration_EndToEnd verifies the signer policy and service
// loaded from the mainnet configuration fixture (#1546):
//  1. Allowed deposit at the exact MaxAmountStroops limit succeeds.
//  2. Deposit over the limit (MaxAmountStroops + 1) is rejected with RejectAmountOutOfPolicy.
//  3. Non-allowlisted contract is rejected with RejectContractNotAllowed.
//  4. Testnet-passphrase intent against the mainnet signer is rejected with RejectNetworkMismatch.
//  5. Stale intent older than MaxIntentAge is rejected with RejectIntentExpired.
func TestMainnetPolicyIntegration_EndToEnd(t *testing.T) {
	fixturePath := filepath.Join("testdata", "mainnet_signer.env")
	env := loadEnvFixture(t, fixturePath)

	// Ensure testdata fixture keys mirror deploy/mainnet/signer.env.example.
	deployExamplePath := filepath.Join("..", "..", "..", "..", "deploy", "mainnet", "signer.env.example")
	deployEnv := loadEnvFixture(t, deployExamplePath)
	for k := range deployEnv {
		if _, ok := env[k]; !ok {
			t.Fatalf("testdata fixture missing key %q from deploy/mainnet/signer.env.example", k)
		}
	}

	policy := buildPolicyFromMainnetFixture(t, env)
	vaultContract := env["CONTRACT_ADDRESS_VAULT"]
	mainnetPassphrase := env["STELLAR_NETWORK_PASSPHRASE"]

	backend := &stubBackend{keyID: "GMAINNETOPERATORSIGNERKEY0000000000000000000000000000000"}
	sink := &recordingSink{}
	ks, _ := newTestKillSwitch(t)

	svc, err := NewService(ServiceOptions{
		Backend:    backend,
		Policy:     policy,
		KillSwitch: ks,
		Sink:       sink,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	now := time.Now().UTC()

	t.Run("allowed deposit at exact limit is signed", func(t *testing.T) {
		intent := &Intent{
			ID:                "mainnet-deposit-at-limit",
			Operation:         OpDeposit,
			Shape:             ShapeI128Pair,
			ContractAddress:   vaultContract,
			NetworkPassphrase: mainnetPassphrase,
			Arg0:              policy.MaxAmountStroops,
			Arg1:              0,
			IssuedAt:          now,
		}
		res, err := svc.Sign(context.Background(), "api", intent)
		if err != nil {
			t.Fatalf("expected deposit at limit (%d stroops) to succeed, got: %v", policy.MaxAmountStroops, err)
		}
		if res.SignedXDR != "SIGNED_ENVELOPE_XDR" {
			t.Fatalf("unexpected SignedXDR = %q", res.SignedXDR)
		}
		if backend.callCount() != 1 {
			t.Fatalf("backend callCount = %d, want 1", backend.callCount())
		}
	})

	t.Run("deposit over the limit is rejected", func(t *testing.T) {
		intent := &Intent{
			ID:                "mainnet-deposit-over-limit",
			Operation:         OpDeposit,
			Shape:             ShapeI128Pair,
			ContractAddress:   vaultContract,
			NetworkPassphrase: mainnetPassphrase,
			Arg0:              policy.MaxAmountStroops + 1,
			Arg1:              0,
			IssuedAt:          now,
		}
		_, err := svc.Sign(context.Background(), "api", intent)
		assertRejected(t, err, RejectAmountOutOfPolicy)
		if backend.callCount() != 1 {
			t.Fatal("over-limit intent must not reach signing backend")
		}
	})

	t.Run("non-allowlisted contract is rejected", func(t *testing.T) {
		intent := &Intent{
			ID:                "mainnet-unlisted-contract",
			Operation:         OpDeposit,
			Shape:             ShapeI128Pair,
			ContractAddress:   "CFUNLISTED2476B3R236R3T7G7M3MMK23R2J4S3I6W3M2A4J6S3I6W3M",
			NetworkPassphrase: mainnetPassphrase,
			Arg0:              10_000_000,
			Arg1:              0,
			IssuedAt:          now,
		}
		_, err := svc.Sign(context.Background(), "api", intent)
		assertRejected(t, err, RejectContractNotAllowed)
	})

	t.Run("testnet passphrase intent against mainnet signer is rejected", func(t *testing.T) {
		intent := &Intent{
			ID:                "mainnet-wrong-passphrase",
			Operation:         OpDeposit,
			Shape:             ShapeI128Pair,
			ContractAddress:   vaultContract,
			NetworkPassphrase: "Test SDF Network ; September 2015",
			Arg0:              10_000_000,
			Arg1:              0,
			IssuedAt:          now,
		}
		_, err := svc.Sign(context.Background(), "api", intent)
		assertRejected(t, err, RejectNetworkMismatch)
	})

	t.Run("stale intent older than MaxIntentAge is rejected", func(t *testing.T) {
		intent := &Intent{
			ID:                "mainnet-stale-intent",
			Operation:         OpDeposit,
			Shape:             ShapeI128Pair,
			ContractAddress:   vaultContract,
			NetworkPassphrase: mainnetPassphrase,
			Arg0:              10_000_000,
			Arg1:              0,
			IssuedAt:          now.Add(-(policy.MaxIntentAge + 5*time.Second)),
		}
		_, err := svc.Sign(context.Background(), "api", intent)
		assertRejected(t, err, RejectIntentExpired)
	})
}
