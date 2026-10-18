package scheduler

import (
	"context"
	"path/filepath"
	"testing"
)

type stubContractConfigReader struct {
	state *ExpectedContractState
	err   error
}

func (s *stubContractConfigReader) ReadLiveContractState(_ context.Context, _, _ string) (*ExpectedContractState, error) {
	return s.state, s.err
}

func TestContractStateVerifier_LoadsManifestAndPassesWhenMatching(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "..", "..", "deploy", "mainnet", "expected-state.json")
	expected, err := LoadExpectedContractState(manifestPath)
	if err != nil {
		t.Fatalf("LoadExpectedContractState: %v", err)
	}

	alertFired := false
	verifier := NewContractStateVerifier(
		ContractStateVerifierConfig{
			Enabled:  true,
			Expected: expected,
		},
		&stubContractConfigReader{state: expected},
		ContractStateDriftAlertFunc(func(_ context.Context, _ string, _ []ContractStateDiff, _ error) {
			alertFired = true
		}),
		nil,
	)

	diffs, err := verifier.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(diffs) != 0 {
		t.Fatalf("expected 0 diffs for matching state, got %v", diffs)
	}
	if alertFired {
		t.Fatal("expected no alert when live state matches manifest")
	}
}

func TestContractStateVerifier_DriftedStateFiresAlert(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "..", "..", "deploy", "mainnet", "expected-state.json")
	expected, err := LoadExpectedContractState(manifestPath)
	if err != nil {
		t.Fatalf("LoadExpectedContractState: %v", err)
	}

	// Clone expected state and introduce drift across roles, caps, fees, pause, and treasury recipient.
	drifted, err := LoadExpectedContractState(manifestPath)
	if err != nil {
		t.Fatalf("LoadExpectedContractState: %v", err)
	}
	drifted.Roles["Admin"] = []string{"GATTACKERUNAUTHORIZEDADMIN000000000000000000000000000000"}
	drifted.DepositCaps.MaxTotalAssetsStroops = 99999999999999
	drifted.FeeConfig.PerformanceFeeBps = 5000
	drifted.FeeConfig.TreasuryRecipient = "CATTACKERTREASURY000000000000000000000000000000000000000"
	drifted.Pause.DepositsPaused = true
	drifted.TimelockDelaySeconds = 60

	var capturedDiffs []ContractStateDiff
	verifier := NewContractStateVerifier(
		ContractStateVerifierConfig{
			Enabled:  true,
			Expected: expected,
		},
		&stubContractConfigReader{state: drifted},
		ContractStateDriftAlertFunc(func(_ context.Context, _ string, diffs []ContractStateDiff, _ error) {
			capturedDiffs = diffs
		}),
		nil,
	)

	diffs, err := verifier.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(diffs) != 6 {
		t.Fatalf("expected 6 diffs on drifted state, got %d: %v", len(diffs), diffs)
	}
	if len(capturedDiffs) != 6 {
		t.Fatalf("expected alerter to receive 6 diffs, got %d", len(capturedDiffs))
	}
}
