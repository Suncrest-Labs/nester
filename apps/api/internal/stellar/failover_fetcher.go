package stellar

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
)

const (
	// DefaultHotRetentionLedgers is the 7-day retention window (~120,960 ledgers
	// at 5s/ledger) offered by public Soroban RPC endpoints such as
	// mainnet.sorobanrpc.com.
	DefaultHotRetentionLedgers uint64 = 120_960

	// FourteenDayRetentionLedgers is 14 days of ledgers (~241,920 ledgers at
	// 5s/ledger), the minimum backfill horizon required by issue #1559.
	FourteenDayRetentionLedgers uint64 = 241_920

	// ThirtyDayRetentionLedgers is 30 days of ledgers (~518,400 ledgers at
	// 5s/ledger), matching the self-hosted stellar-rpc HISTORY_RETENTION_WINDOW.
	ThirtyDayRetentionLedgers uint64 = 518_400
)

// NamedEventFetcher pairs an EventFetcher with its provider URL or identifier
// for ordered failover logging and diagnostics.
type NamedEventFetcher struct {
	Name    string
	Fetcher EventFetcher
	Archive bool
}

// FailoverEventFetcher implements EventFetcher across an ordered chain of
// Soroban RPC providers (primary -> fallbacks -> long-retention archive).
//
// Routing rules (nester#1559):
//  1. When the requested startLedger lags behind the last observed network tip
//     by more than HotRetentionLedgers (default 120,960 ledgers / ~7 days) and
//     an archive provider is configured, FetchEvents routes directly to the
//     archive provider first so a 14-day backfill does not hammer a 7-day RPC.
//  2. Otherwise providers are tried strictly in configured order:
//     primary -> fallback[0..N] -> archive.
//  3. Any RPC error (including retention-window rejection such as
//     "startLedger is before oldest ledger") triggers immediate failover to the
//     next provider in the chain.
type FailoverEventFetcher struct {
	Providers           []NamedEventFetcher
	HotRetentionLedgers uint64
	Logger              *slog.Logger

	lastObservedTip atomic.Uint64
}

// NewFailoverEventFetcherWithOptions builds an EventFetcher from the primary
// RPC URL, optional ordered fallback URLs, and optional long-retention archive
// RPC URL. If no fallback or archive URLs are configured, it returns a single
// rpcEventFetcher.
func NewFailoverEventFetcherWithOptions(
	client *http.Client,
	primaryURL string,
	fallbackURLs []string,
	archiveURL string,
	opts RPCOptions,
	logger *slog.Logger,
) EventFetcher {
	seen := make(map[string]int)
	providers := make([]NamedEventFetcher, 0, 2+len(fallbackURLs))

	add := func(rawURL string, isArchive bool) {
		u := strings.TrimSpace(rawURL)
		if u == "" {
			return
		}
		if idx, exists := seen[u]; exists {
			if isArchive {
				providers[idx].Archive = true
			}
			return
		}
		seen[u] = len(providers)
		providers = append(providers, NamedEventFetcher{
			Name:    u,
			Fetcher: NewRPCEventFetcherWithOptions(client, u, opts),
			Archive: isArchive,
		})
	}

	add(primaryURL, false)
	for _, fb := range fallbackURLs {
		add(fb, false)
	}
	add(archiveURL, true)

	if len(providers) == 1 && !providers[0].Archive {
		return providers[0].Fetcher
	}

	return &FailoverEventFetcher{
		Providers:           providers,
		HotRetentionLedgers: DefaultHotRetentionLedgers,
		Logger:              logger,
	}
}

func (f *FailoverEventFetcher) retentionThreshold() uint64 {
	if f.HotRetentionLedgers > 0 {
		return f.HotRetentionLedgers
	}
	return DefaultHotRetentionLedgers
}

// orderedForStartLedger returns the provider evaluation order for a given
// startLedger. When startLedger is older than the hot retention window relative
// to the known network tip, archive-capable providers are promoted to the front.
func (f *FailoverEventFetcher) orderedForStartLedger(startLedger uint64) []NamedEventFetcher {
	if len(f.Providers) <= 1 {
		return f.Providers
	}

	tip := f.lastObservedTip.Load()
	threshold := f.retentionThreshold()
	needsArchive := tip > 0 && startLedger > 0 && tip > startLedger && (tip-startLedger) > threshold

	if !needsArchive {
		return f.Providers
	}

	promoted := make([]NamedEventFetcher, 0, len(f.Providers))
	for _, p := range f.Providers {
		if p.Archive {
			promoted = append(promoted, p)
		}
	}
	if len(promoted) == 0 {
		return f.Providers
	}
	for _, p := range f.Providers {
		if !p.Archive {
			promoted = append(promoted, p)
		}
	}
	return promoted
}

// FetchEvents queries providers in priority order (or archive-first when
// backfilling beyond the 7-day hot retention window).
func (f *FailoverEventFetcher) FetchEvents(
	ctx context.Context,
	contractIDs []string,
	startLedger uint64,
) ([]indexedEvent, uint64, error) {
	if len(f.Providers) == 0 {
		return nil, 0, fmt.Errorf("failover event fetcher: no RPC providers configured")
	}

	candidates := f.orderedForStartLedger(startLedger)
	var errs []string

	for i, candidate := range candidates {
		events, latestLedger, err := candidate.Fetcher.FetchEvents(ctx, contractIDs, startLedger)
		if err == nil {
			if latestLedger > 0 {
				f.recordTip(latestLedger)
			}
			if i > 0 && f.Logger != nil {
				f.Logger.Info("soroban rpc failover succeeded",
					"provider", candidate.Name,
					"archive", candidate.Archive,
					"start_ledger", startLedger,
					"events_fetched", len(events),
				)
			}
			return events, latestLedger, nil
		}

		errs = append(errs, fmt.Sprintf("%s: %v", candidate.Name, err))
		if f.Logger != nil {
			f.Logger.Warn("soroban rpc provider failed, trying next failover provider",
				"failed_provider", candidate.Name,
				"attempt", i+1,
				"total_providers", len(candidates),
				"start_ledger", startLedger,
				"error", err,
			)
		}
	}

	return nil, 0, fmt.Errorf("all soroban rpc providers failed (%s)", strings.Join(errs, "; "))
}

// LatestLedger queries providers in priority order and caches the highest
// observed tip so subsequent FetchEvents calls can detect deep backfill gaps.
func (f *FailoverEventFetcher) LatestLedger(ctx context.Context) (uint64, error) {
	if len(f.Providers) == 0 {
		return 0, fmt.Errorf("failover event fetcher: no RPC providers configured")
	}

	var errs []string
	for _, candidate := range f.Providers {
		tip, err := candidate.Fetcher.LatestLedger(ctx)
		if err == nil && tip > 0 {
			f.recordTip(tip)
			return tip, nil
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", candidate.Name, err))
		} else {
			errs = append(errs, fmt.Sprintf("%s: latest ledger 0", candidate.Name))
		}
	}

	return 0, fmt.Errorf("all soroban rpc providers failed LatestLedger (%s)", strings.Join(errs, "; "))
}

func (f *FailoverEventFetcher) recordTip(tip uint64) {
	for {
		cur := f.lastObservedTip.Load()
		if tip <= cur {
			return
		}
		if f.lastObservedTip.CompareAndSwap(cur, tip) {
			return
		}
	}
}
