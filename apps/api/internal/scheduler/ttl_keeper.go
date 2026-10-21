package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/stellar/go/strkey"
	"github.com/stellar/go/xdr"

	"github.com/suncrestlabs/nester/apps/api/internal/metrics"
)

// Default TTL constants matching packages/contracts/STORAGE_TTL_STRATEGY.md.
const (
	// DefaultTTLThresholdLedgers is the minimum remaining ledger count before extending (~7 days).
	DefaultTTLThresholdLedgers uint32 = 120_960

	// DefaultTTLExtendToLedgers is the number of ledgers to extend TTL to (~30 days).
	DefaultTTLExtendToLedgers uint32 = 518_400
)

// TTLKeeperConfig controls the TTL keeper scheduler job (nester#1486).
type TTLKeeperConfig struct {
	Enabled                bool
	Interval               time.Duration
	ExpiryThresholdLedgers uint32
	ExtendToLedgers        uint32
	ContractAddresses      []string
	KnownUserAddresses     []string
}

// EntryTTLInfo contains the parsed TTL information for an entry.
type EntryTTLInfo struct {
	Key                xdr.LedgerKey
	ContractAddress    string
	Kind               string // "instance" or "user"
	LiveUntilLedgerSeq uint32
}

// TTLKeeperChain is the chain query and execution interface required by the TTL keeper.
type TTLKeeperChain interface {
	// GetLatestLedger returns the current ledger sequence on the network.
	GetLatestLedger(ctx context.Context) (uint32, error)

	// BuildMonitoredKeys builds the LedgerKeys for contract instances and user keys.
	BuildMonitoredKeys(ctx context.Context, contractAddresses []string, userAddresses []string) ([]xdr.LedgerKey, error)

	// GetLedgerEntriesTTL queries getLedgerEntries and returns entry details and liveUntilLedgerSeq.
	GetLedgerEntriesTTL(ctx context.Context, keys []xdr.LedgerKey) ([]EntryTTLInfo, error)

	// ExtendTTL submits an ExtendFootprintTtl operation or extend_ttl contract call.
	ExtendTTL(ctx context.Context, keys []xdr.LedgerKey, extendTo uint32) (txHash string, err error)
}

// TTLKeeperJob is the leader-elected scheduler job that prevents contract entries
// from becoming archived by extending their TTL before expiry.
type TTLKeeperJob struct {
	cfg     TTLKeeperConfig
	chain   TTLKeeperChain
	metrics *metrics.Metrics
	logger  *slog.Logger
	leader  LeaderChecker
}

// NewTTLKeeperJob constructs a new TTL keeper job.
func NewTTLKeeperJob(cfg TTLKeeperConfig, chain TTLKeeperChain, m *metrics.Metrics, logger *slog.Logger) *TTLKeeperJob {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	if cfg.ExpiryThresholdLedgers == 0 {
		cfg.ExpiryThresholdLedgers = DefaultTTLThresholdLedgers
	}
	if cfg.ExtendToLedgers == 0 {
		cfg.ExtendToLedgers = DefaultTTLExtendToLedgers
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 1 * time.Hour
	}
	return &TTLKeeperJob{
		cfg:     cfg,
		chain:   chain,
		metrics: m,
		logger:  logger,
	}
}

// SetLeaderChecker installs leader election on the keeper job.
func (j *TTLKeeperJob) SetLeaderChecker(l LeaderChecker) { j.leader = l }

func (j *TTLKeeperJob) isLeader() bool {
	return j.leader == nil || j.leader.IsLeader()
}

// Run executes the keeper loop until ctx is cancelled.
func (j *TTLKeeperJob) Run(ctx context.Context) {
	if !j.cfg.Enabled {
		j.logger.Info("ttl keeper job disabled")
		return
	}

	j.logger.Info("ttl keeper job starting",
		"interval", j.cfg.Interval,
		"contracts", len(j.cfg.ContractAddresses),
		"users", len(j.cfg.KnownUserAddresses),
		"threshold_ledgers", j.cfg.ExpiryThresholdLedgers,
	)

	_, _ = j.Step(ctx)

	ticker := time.NewTicker(j.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			j.logger.Info("ttl keeper job stopping")
			return
		case <-ticker.C:
			_, _ = j.Step(ctx)
		}
	}
}

// Step performs a single check-and-extend cycle.
func (j *TTLKeeperJob) Step(ctx context.Context) (int, error) {
	if !j.isLeader() {
		j.logger.Debug("ttl keeper skipping run: not leader")
		return 0, nil
	}

	latestLedger, err := j.chain.GetLatestLedger(ctx)
	if err != nil {
		j.logger.Error("ttl keeper failed to get latest ledger", "error", err)
		return 0, fmt.Errorf("get latest ledger: %w", err)
	}

	keys, err := j.chain.BuildMonitoredKeys(ctx, j.cfg.ContractAddresses, j.cfg.KnownUserAddresses)
	if err != nil {
		j.logger.Error("ttl keeper failed to build monitored keys", "error", err)
		return 0, fmt.Errorf("build monitored keys: %w", err)
	}

	if len(keys) == 0 {
		j.logger.Debug("ttl keeper: no keys configured to monitor")
		return 0, nil
	}

	entries, err := j.chain.GetLedgerEntriesTTL(ctx, keys)
	if err != nil {
		j.logger.Error("ttl keeper failed to read ledger entries TTL", "error", err)
		return 0, fmt.Errorf("get ledger entries ttl: %w", err)
	}

	var toExtend []xdr.LedgerKey

	for _, entry := range entries {
		var ledgersRemaining uint32
		if entry.LiveUntilLedgerSeq > latestLedger {
			ledgersRemaining = entry.LiveUntilLedgerSeq - latestLedger
		}

		if j.metrics != nil {
			j.metrics.SetContractEntryTTLLedgersRemaining(entry.ContractAddress, entry.Kind, float64(ledgersRemaining))
		}

		if ledgersRemaining <= j.cfg.ExpiryThresholdLedgers {
			toExtend = append(toExtend, entry.Key)
		}
	}

	if len(toExtend) == 0 {
		j.logger.Info("ttl keeper: all monitored entries healthy",
			"total_checked", len(entries),
			"latest_ledger", latestLedger,
		)
		return 0, nil
	}

	j.logger.Info("ttl keeper: extending expiring entries",
		"count", len(toExtend),
		"threshold_ledgers", j.cfg.ExpiryThresholdLedgers,
	)

	extendTo := latestLedger + j.cfg.ExtendToLedgers
	txHash, err := j.chain.ExtendTTL(ctx, toExtend, extendTo)
	if err != nil {
		j.logger.Error("ttl keeper: extend transaction failed", "error", err, "count", len(toExtend))
		return 0, fmt.Errorf("extend ttl: %w", err)
	}

	j.logger.Info("ttl keeper: successfully extended entries",
		"tx_hash", txHash,
		"count", len(toExtend),
		"extend_to_ledger", extendTo,
	)

	// Refresh metrics after successful extension so the metrics show TTL rising
	updatedEntries, err := j.chain.GetLedgerEntriesTTL(ctx, toExtend)
	if err == nil {
		for _, entry := range updatedEntries {
			var ledgersRemaining uint32
			if entry.LiveUntilLedgerSeq > latestLedger {
				ledgersRemaining = entry.LiveUntilLedgerSeq - latestLedger
			}
			if j.metrics != nil {
				j.metrics.SetContractEntryTTLLedgersRemaining(entry.ContractAddress, entry.Kind, float64(ledgersRemaining))
			}
		}
	}

	return len(toExtend), nil
}

// BuildDefaultMonitoredKeys builds standard LedgerKeys for contract instances and user balances.
func BuildDefaultMonitoredKeys(contractAddresses []string, userAddresses []string) ([]xdr.LedgerKey, error) {
	var keys []xdr.LedgerKey

	for _, contractAddr := range contractAddresses {
		contractIDBytes, err := strkey.Decode(strkey.VersionByteContract, contractAddr)
		if err != nil {
			return nil, fmt.Errorf("invalid contract address %q: %w", contractAddr, err)
		}
		var contractId xdr.ContractId
		copy(contractId[:], contractIDBytes)

		// 1. Contract instance key
		instanceKey := xdr.LedgerKey{
			Type: xdr.LedgerEntryTypeContractData,
			ContractData: &xdr.LedgerKeyContractData{
				Contract: xdr.ScAddress{
					Type:       xdr.ScAddressTypeScAddressTypeContract,
					ContractId: &contractId,
				},
				Key: xdr.ScVal{
					Type: xdr.ScValTypeScvLedgerKeyContractInstance,
				},
				Durability: xdr.ContractDataDurabilityPersistent,
			},
		}
		keys = append(keys, instanceKey)

		// 2. User persistent keys
		for _, userAddr := range userAddresses {
			accId, err := xdr.AddressToAccountId(userAddr)
			if err != nil {
				continue
			}
			userScAddress := xdr.ScAddress{
				Type:      xdr.ScAddressTypeScAddressTypeAccount,
				AccountId: &accId,
			}
			keySymbol := xdr.ScSymbol("UserPrincipal")
			vec := xdr.ScVec{
				xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &keySymbol},
				xdr.ScVal{Type: xdr.ScValTypeScvAddress, Address: &userScAddress},
			}
			vecPtr := &vec
			userKey := xdr.LedgerKey{
				Type: xdr.LedgerEntryTypeContractData,
				ContractData: &xdr.LedgerKeyContractData{
					Contract: xdr.ScAddress{
						Type:       xdr.ScAddressTypeScAddressTypeContract,
						ContractId: &contractId,
					},
					Key: xdr.ScVal{
						Type: xdr.ScValTypeScvVec,
						Vec:  &vecPtr,
					},
					Durability: xdr.ContractDataDurabilityPersistent,
				},
			}
			keys = append(keys, userKey)
		}
	}

	return keys, nil
}
