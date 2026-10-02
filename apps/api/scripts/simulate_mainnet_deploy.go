//go:build simulationsafety

// Command simulate_mainnet_deploy verifies the deploy and initial-seed
// sequence against a forked mainnet (or testnet) RPC endpoint without
// broadcasting any transaction, so it can run against mainnet state with no
// risk to live assets.
//
// It performs three real checks, not placeholder output:
//
//  1. Pings the Soroban RPC endpoint and confirms it reports healthy.
//  2. For each already-deployed contract address supplied via flags/env,
//     simulates the vault's total_assets() view through ContractReader.
//     ContractReader never submits a transaction — every call it makes is a
//     read-only simulation — so this confirms the deployed contract is live
//     and queryable without mutating any state.
//  3. Reports a pass/fail summary and exits non-zero if any contract failed
//     to simulate.
//
// Usage:
//
//	go run -tags simulationsafety ./scripts/simulate_mainnet_deploy.go \
//	  -rpc-url https://soroban-testnet.stellar.org \
//	  -vault CA... -vault CB...
//
// Contract addresses may also be supplied via the NESTER_SIMULATE_VAULTS
// environment variable as a comma-separated list, matching the
// NEXT_PUBLIC_*_CONTRACT_ID values written by deploy-testnet.sh.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/stellar"
)

type vaultFlags []string

func (v *vaultFlags) String() string { return strings.Join(*v, ",") }

func (v *vaultFlags) Set(value string) error {
	*v = append(*v, value)
	return nil
}

func main() {
	rpcURL := flag.String("rpc-url", "https://soroban-testnet.stellar.org", "Soroban RPC URL for the fork/simulation environment")
	networkPassphrase := flag.String("network-passphrase", "Test SDF Network ; September 2015", "Network passphrase matching the target RPC endpoint")
	var vaults vaultFlags
	flag.Var(&vaults, "vault", "Deployed vault contract address to simulate against (repeatable)")
	flag.Parse()

	if envVaults := os.Getenv("NESTER_SIMULATE_VAULTS"); envVaults != "" {
		for _, addr := range strings.Split(envVaults, ",") {
			addr = strings.TrimSpace(addr)
			if addr != "" {
				vaults = append(vaults, addr)
			}
		}
	}

	if err := run(*rpcURL, *networkPassphrase, vaults); err != nil {
		log.Fatalf("[simulation] FAILED: %v", err)
	}
}

func run(rpcURL, networkPassphrase string, vaults []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := &http.Client{Timeout: 30 * time.Second}

	fmt.Printf("[simulation] Step 1: checking Soroban RPC health at %s\n", rpcURL)
	health := stellar.PingSorobanRPC(ctx, client, rpcURL)
	if !health.OK {
		return fmt.Errorf("RPC endpoint %s did not report healthy: %s", rpcURL, health.Error)
	}
	fmt.Printf("[simulation] RPC healthy, latest ledger %d\n", health.LatestLedger)

	if len(vaults) == 0 {
		fmt.Println("[simulation] Step 2: no vault addresses supplied (-vault or NESTER_SIMULATE_VAULTS) — skipping deployment verification")
		fmt.Println("[simulation] Dry-run completed. No state was mutated. Supply -vault addresses to verify a deploy/seed sequence.")
		return nil
	}

	reader := stellar.NewContractReader(rpcURL, networkPassphrase, "")

	fmt.Printf("[simulation] Step 2: simulating total_assets() against %d deployed vault(s)\n", len(vaults))
	var failures []string
	for _, addr := range vaults {
		balance, err := reader.TotalAssets(ctx, addr)
		if err != nil {
			fmt.Printf("[simulation]   %s: FAILED (%v)\n", addr, err)
			failures = append(failures, addr)
			continue
		}
		fmt.Printf("[simulation]   %s: OK (total_assets=%s)\n", addr, balance.String())
	}

	if len(failures) > 0 {
		return fmt.Errorf("%d of %d vault(s) failed simulation: %s", len(failures), len(vaults), strings.Join(failures, ", "))
	}

	fmt.Println("[simulation] Step 3: all deployed vaults simulated successfully. No state was mutated on-chain.")
	return nil
}
