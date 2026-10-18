package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"sort"
	"time"
)

// DepositCapsState represents the on-chain deposit and TVL cap parameters.
type DepositCapsState struct {
	MinDepositStroops     int64 `json:"min_deposit_stroops"`
	MaxDepositStroops     int64 `json:"max_deposit_stroops"`
	MaxTotalAssetsStroops int64 `json:"max_total_assets_stroops"`
}

// FeeConfigState represents the on-chain fee parameters and treasury recipient.
type FeeConfigState struct {
	PerformanceFeeBps uint32 `json:"performance_fee_bps"`
	ManagementFeeBps  uint32 `json:"management_fee_bps"`
	WithdrawalFeeBps  uint32 `json:"withdrawal_fee_bps"`
	TreasuryRecipient string `json:"treasury_recipient"`
}

// PauseState represents the on-chain pause and circuit-breaker flags.
type PauseState struct {
	DepositsPaused    bool `json:"deposits_paused"`
	WithdrawalsPaused bool `json:"withdrawals_paused"`
	EmergencyMode     bool `json:"emergency_mode"`
}

// ExpectedContractState represents the canonical on-chain configuration
// snapshot from `deploy/mainnet/expected-state.json`.
type ExpectedContractState struct {
	NetworkPassphrase    string              `json:"network_passphrase"`
	VaultContractID      string              `json:"vault_contract_id"`
	TimelockContractID   string              `json:"timelock_contract_id"`
	Admin                string              `json:"admin"`
	Roles                map[string][]string `json:"roles"`
	DepositCaps          DepositCapsState    `json:"deposit_caps"`
	FeeConfig            FeeConfigState      `json:"fee_config"`
	Pause                PauseState          `json:"pause"`
	AllowlistMode        bool                `json:"allowlist_mode"`
	Adapters             []string            `json:"adapters"`
	TimelockDelaySeconds uint64              `json:"timelock_delay_seconds"`
}

// LoadExpectedContractState parses an expected-state JSON manifest from disk.
func LoadExpectedContractState(path string) (*ExpectedContractState, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- operator-supplied manifest path
	if err != nil {
		return nil, fmt.Errorf("read expected state manifest %s: %w", path, err)
	}
	var state ExpectedContractState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("parse expected state manifest %s: %w", path, err)
	}
	return &state, nil
}

// ContractStateDiff describes a single field mismatch between the expected
// manifest and the live on-chain state.
type ContractStateDiff struct {
	Field    string
	Expected any
	Actual   any
}

func (d ContractStateDiff) String() string {
	return fmt.Sprintf("%s: expected=%v actual=%v", d.Field, d.Expected, d.Actual)
}

// CompareContractState compares an expected manifest against the live on-chain
// state and returns all detected differences across admin, roles, deposit caps,
// fee config, treasury recipient, pause flags, allowlist mode, adapters, and
// timelock delay.
func CompareContractState(expected, actual *ExpectedContractState) []ContractStateDiff {
	var diffs []ContractStateDiff
	if expected == nil || actual == nil {
		diffs = append(diffs, ContractStateDiff{Field: "manifest", Expected: expected != nil, Actual: actual != nil})
		return diffs
	}

	if expected.Admin != actual.Admin {
		diffs = append(diffs, ContractStateDiff{Field: "admin", Expected: expected.Admin, Actual: actual.Admin})
	}

	roleKeys := make(map[string]struct{})
	for k := range expected.Roles {
		roleKeys[k] = struct{}{}
	}
	for k := range actual.Roles {
		roleKeys[k] = struct{}{}
	}
	sortedRoles := make([]string, 0, len(roleKeys))
	for k := range roleKeys {
		sortedRoles = append(sortedRoles, k)
	}
	sort.Strings(sortedRoles)

	for _, role := range sortedRoles {
		expHolders := sortedCopy(expected.Roles[role])
		actHolders := sortedCopy(actual.Roles[role])
		if !reflect.DeepEqual(expHolders, actHolders) {
			diffs = append(diffs, ContractStateDiff{
				Field:    "roles." + role,
				Expected: expHolders,
				Actual:   actHolders,
			})
		}
	}

	if expected.DepositCaps.MinDepositStroops != actual.DepositCaps.MinDepositStroops {
		diffs = append(diffs, ContractStateDiff{
			Field:    "deposit_caps.min_deposit_stroops",
			Expected: expected.DepositCaps.MinDepositStroops,
			Actual:   actual.DepositCaps.MinDepositStroops,
		})
	}
	if expected.DepositCaps.MaxDepositStroops != actual.DepositCaps.MaxDepositStroops {
		diffs = append(diffs, ContractStateDiff{
			Field:    "deposit_caps.max_deposit_stroops",
			Expected: expected.DepositCaps.MaxDepositStroops,
			Actual:   actual.DepositCaps.MaxDepositStroops,
		})
	}
	if expected.DepositCaps.MaxTotalAssetsStroops != actual.DepositCaps.MaxTotalAssetsStroops {
		diffs = append(diffs, ContractStateDiff{
			Field:    "deposit_caps.max_total_assets_stroops",
			Expected: expected.DepositCaps.MaxTotalAssetsStroops,
			Actual:   actual.DepositCaps.MaxTotalAssetsStroops,
		})
	}

	if expected.FeeConfig.PerformanceFeeBps != actual.FeeConfig.PerformanceFeeBps {
		diffs = append(diffs, ContractStateDiff{
			Field:    "fee_config.performance_fee_bps",
			Expected: expected.FeeConfig.PerformanceFeeBps,
			Actual:   actual.FeeConfig.PerformanceFeeBps,
		})
	}
	if expected.FeeConfig.ManagementFeeBps != actual.FeeConfig.ManagementFeeBps {
		diffs = append(diffs, ContractStateDiff{
			Field:    "fee_config.management_fee_bps",
			Expected: expected.FeeConfig.ManagementFeeBps,
			Actual:   actual.FeeConfig.ManagementFeeBps,
		})
	}
	if expected.FeeConfig.WithdrawalFeeBps != actual.FeeConfig.WithdrawalFeeBps {
		diffs = append(diffs, ContractStateDiff{
			Field:    "fee_config.withdrawal_fee_bps",
			Expected: expected.FeeConfig.WithdrawalFeeBps,
			Actual:   actual.FeeConfig.WithdrawalFeeBps,
		})
	}
	if expected.FeeConfig.TreasuryRecipient != actual.FeeConfig.TreasuryRecipient {
		diffs = append(diffs, ContractStateDiff{
			Field:    "fee_config.treasury_recipient",
			Expected: expected.FeeConfig.TreasuryRecipient,
			Actual:   actual.FeeConfig.TreasuryRecipient,
		})
	}

	if expected.Pause.DepositsPaused != actual.Pause.DepositsPaused {
		diffs = append(diffs, ContractStateDiff{
			Field:    "pause.deposits_paused",
			Expected: expected.Pause.DepositsPaused,
			Actual:   actual.Pause.DepositsPaused,
		})
	}
	if expected.Pause.WithdrawalsPaused != actual.Pause.WithdrawalsPaused {
		diffs = append(diffs, ContractStateDiff{
			Field:    "pause.withdrawals_paused",
			Expected: expected.Pause.WithdrawalsPaused,
			Actual:   actual.Pause.WithdrawalsPaused,
		})
	}
	if expected.Pause.EmergencyMode != actual.Pause.EmergencyMode {
		diffs = append(diffs, ContractStateDiff{
			Field:    "pause.emergency_mode",
			Expected: expected.Pause.EmergencyMode,
			Actual:   actual.Pause.EmergencyMode,
		})
	}

	if expected.AllowlistMode != actual.AllowlistMode {
		diffs = append(diffs, ContractStateDiff{
			Field:    "allowlist_mode",
			Expected: expected.AllowlistMode,
			Actual:   actual.AllowlistMode,
		})
	}

	expAdapters := sortedCopy(expected.Adapters)
	actAdapters := sortedCopy(actual.Adapters)
	if !reflect.DeepEqual(expAdapters, actAdapters) {
		diffs = append(diffs, ContractStateDiff{
			Field:    "adapters",
			Expected: expAdapters,
			Actual:   actAdapters,
		})
	}

	if expected.TimelockDelaySeconds != actual.TimelockDelaySeconds {
		diffs = append(diffs, ContractStateDiff{
			Field:    "timelock_delay_seconds",
			Expected: expected.TimelockDelaySeconds,
			Actual:   actual.TimelockDelaySeconds,
		})
	}

	return diffs
}

func sortedCopy(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)
	return out
}

// ContractConfigReader reads live contract state via simulated Soroban RPC calls.
type ContractConfigReader interface {
	ReadLiveContractState(ctx context.Context, vaultContractID, timelockContractID string) (*ExpectedContractState, error)
}

// ContractStateDriftAlerter is invoked whenever live on-chain state drifts from
// `deploy/mainnet/expected-state.json`.
type ContractStateDriftAlerter interface {
	AlertContractStateDrift(ctx context.Context, vaultContractID string, diffs []ContractStateDiff, readErr error)
}

// ContractStateDriftAlertFunc adapts a function to ContractStateDriftAlerter.
type ContractStateDriftAlertFunc func(ctx context.Context, vaultContractID string, diffs []ContractStateDiff, readErr error)

func (f ContractStateDriftAlertFunc) AlertContractStateDrift(ctx context.Context, vaultContractID string, diffs []ContractStateDiff, readErr error) {
	f(ctx, vaultContractID, diffs, readErr)
}

// ContractStateVerifierConfig configures the scheduled expected-state verifier.
type ContractStateVerifierConfig struct {
	Enabled  bool
	Interval time.Duration
	Expected *ExpectedContractState
}

// ContractStateVerifier periodically compares live on-chain contract config
// against the expected-state manifest and alerts on any drift.
type ContractStateVerifier struct {
	cfg     ContractStateVerifierConfig
	reader  ContractConfigReader
	alerter ContractStateDriftAlerter
	logger  *slog.Logger
}

// NewContractStateVerifier constructs a ContractStateVerifier.
func NewContractStateVerifier(
	cfg ContractStateVerifierConfig,
	reader ContractConfigReader,
	alerter ContractStateDriftAlerter,
	logger *slog.Logger,
) *ContractStateVerifier {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &ContractStateVerifier{
		cfg:     cfg,
		reader:  reader,
		alerter: alerter,
		logger:  logger,
	}
}

// RunOnce executes a single expected-state comparison pass.
func (v *ContractStateVerifier) RunOnce(ctx context.Context) ([]ContractStateDiff, error) {
	if v.cfg.Expected == nil {
		return nil, fmt.Errorf("expected contract state manifest is nil")
	}
	live, err := v.reader.ReadLiveContractState(ctx, v.cfg.Expected.VaultContractID, v.cfg.Expected.TimelockContractID)
	if err != nil {
		if v.alerter != nil {
			v.alerter.AlertContractStateDrift(ctx, v.cfg.Expected.VaultContractID, nil, err)
		}
		return nil, err
	}
	diffs := CompareContractState(v.cfg.Expected, live)
	if len(diffs) > 0 {
		v.logger.Error("contract expected-state drift detected",
			"vault_contract_id", v.cfg.Expected.VaultContractID,
			"diff_count", len(diffs),
		)
		if v.alerter != nil {
			v.alerter.AlertContractStateDrift(ctx, v.cfg.Expected.VaultContractID, diffs, nil)
		}
	}
	return diffs, nil
}

// Run executes the verification loop until ctx is cancelled.
func (v *ContractStateVerifier) Run(ctx context.Context) {
	if !v.cfg.Enabled {
		return
	}
	interval := v.cfg.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	_, _ = v.RunOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = v.RunOnce(ctx)
		}
	}
}
