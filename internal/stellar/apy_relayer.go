package stellar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"math"
	"sync"
	"time"
)

// validProtocolID matches safe protocol identifiers (alphanumeric, dash, underscore, 1–64 chars).
var validProtocolID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// privateIPBlocks lists all RFC-reserved private and loopback ranges used for SSRF prevention.
var privateIPBlocks = func() []*net.IPNet {
	blocks := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"::1/128",
		"fc00::/7",
		"169.254.0.0/16", // link-local / AWS IMDS
	}
	nets := make([]*net.IPNet, 0, len(blocks))
	for _, b := range blocks {
		_, network, _ := net.ParseCIDR(b)
		nets = append(nets, network)
	}
	return nets
}()

func isPrivateIP(ip net.IP) bool {
	for _, block := range privateIPBlocks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// ssrfSafeTransport returns an *http.Transport whose DialContext blocks requests
// to private/loopback IP ranges to prevent SSRF.
func ssrfSafeTransport() *http.Transport {
	dialer := &net.Dialer{}
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid address %q: %w", addr, err)
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("DNS lookup failed for %q: %w", host, err)
			}
			for _, ip := range ips {
				if isPrivateIP(ip) {
					return nil, fmt.Errorf("SSRF protection: blocked request to private/reserved IP %s", ip)
				}
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
}

const defaultAPYStalenessThreshold = time.Hour

const (
	MinAPYBPS = 0
	MaxAPYBPS = 10_000
)

type APYQuote struct {
	ProtocolID string
	APYBPS     uint32
	UpdatedAt  time.Time
	Source     string
}

type APYSource interface {
	Name() string
	ProtocolIDs() []string
	Fetch(ctx context.Context) ([]APYQuote, error)
}

type APYUpdater interface {
	UpdateAPY(ctx context.Context, registryID string, protocolID string, apyBPS uint32) error
}

type StaleAPYAlert struct {
	ProtocolID    string
	LastUpdatedAt time.Time
	Age           time.Duration
	Threshold     time.Duration
}

type APYRelayer struct {
	updater            APYUpdater
	registryID         string
	sources            []APYSource
	interval           time.Duration
	stalenessThreshold time.Duration
	onStale            func(StaleAPYAlert)
	onError            func(error)
	now                func() time.Time

	mu           sync.Mutex
	lastUpdated  map[string]time.Time
	staleAlerted map[string]bool
	// Track last-applied APY per protocol (bps) for deviation checks
	lastAPY map[string]uint32
	// Maximum allowed relative deviation (e.g., 0.5 == 50%) between
	// incoming APY and last stored APY before requiring manual review.
	maxDeviationPct float64
	// Minimum number of independent sources required to accept an aggregated APY
	minSources int
}

func NewAPYRelayer(
	updater APYUpdater,
	registryID string,
	sources []APYSource,
	interval time.Duration,
	stalenessThreshold time.Duration,
	onStale func(StaleAPYAlert),
) (*APYRelayer, error) {
	if updater == nil {
		return nil, errors.New("apy updater is required")
	}
	if strings.TrimSpace(registryID) == "" {
		return nil, errors.New("registry ID is required")
	}
	if len(sources) < 2 {
		return nil, errors.New("at least two APY sources are required")
	}
	if interval <= 0 {
		return nil, errors.New("interval must be greater than zero")
	}
	if stalenessThreshold <= 0 {
		stalenessThreshold = defaultAPYStalenessThreshold
	}

	return &APYRelayer{
		updater:            updater,
		registryID:         registryID,
		sources:            sources,
		interval:           interval,
		stalenessThreshold: stalenessThreshold,
		onStale:            onStale,
		now:                time.Now,
		lastUpdated:        make(map[string]time.Time),
		staleAlerted:       make(map[string]bool),
		lastAPY:            make(map[string]uint32),
		maxDeviationPct:    0.5, // default 50% allowed single-update deviation
		minSources:         2,
	}, nil
}

// medianUint32 returns the median of the input slice. If even length, returns the rounded average.
func medianUint32(vals []uint32) uint32 {
	if len(vals) == 0 {
		return 0
	}
	// copy and sort
	s := make([]uint32, len(vals))
	copy(s, vals)
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	a := float64(s[n/2-1])
	b := float64(s[n/2])
	return uint32(math.Round((a + b) / 2.0))
}

// collectGroupedQuotes gathers quotes from all sources grouped by protocol id.
// It returns a map[protocolID] -> []APYQuote and an aggregated error (if any source errors occurred).
func (r *APYRelayer) collectGroupedQuotes(ctx context.Context) (map[string][]APYQuote, error) {
	grouped := make(map[string][]APYQuote)
	var errs []error

	for _, source := range r.sources {
		quotes, err := source.Fetch(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s source fetch failed: %w", source.Name(), err))
			continue
		}
		for _, q := range quotes {
			if strings.TrimSpace(q.ProtocolID) == "" {
				continue
			}
			if q.Source == "" {
				q.Source = source.Name()
			}
			if q.UpdatedAt.IsZero() {
				q.UpdatedAt = r.now().UTC()
			}
			grouped[q.ProtocolID] = append(grouped[q.ProtocolID], q)
		}
	}

	if len(errs) == 0 {
		return grouped, nil
	}
	return grouped, errors.Join(errs...)
}

// aggregateGroupedQuotes computes the median APY per protocol from grouped quotes
// after applying bounds filtering and min source checks. Returned map includes
// only protocols that met the min source requirement.
func (r *APYRelayer) aggregateGroupedQuotes(grouped map[string][]APYQuote) (map[string]APYQuote, error) {
	out := make(map[string]APYQuote)
	var errs []error

	for pid, quotes := range grouped {
		var valid []uint32
		var latestTime time.Time
		var latestSource string
		for _, q := range quotes {
			if q.APYBPS < MinAPYBPS || q.APYBPS > MaxAPYBPS {
				// skip out-of-bounds
				continue
			}
			valid = append(valid, q.APYBPS)
			if q.UpdatedAt.After(latestTime) {
				latestTime = q.UpdatedAt
				latestSource = q.Source
			}
		}
		if len(valid) < r.minSources {
			errs = append(errs, fmt.Errorf("insufficient valid APY sources for %s: got %d, need %d", pid, len(valid), r.minSources))
			continue
		}
		med := medianUint32(valid)
		out[pid] = APYQuote{ProtocolID: pid, APYBPS: med, UpdatedAt: latestTime, Source: latestSource}
	}

	if len(errs) == 0 {
		return out, nil
	}
	return out, errors.Join(errs...)
}

func (r *APYRelayer) SetErrorHandler(handler func(error)) {
	r.onError = handler
}

func (r *APYRelayer) Start(ctx context.Context) error {
	if err := r.RunOnce(ctx); err != nil && r.onError != nil {
		r.onError(err)
	}

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.RunOnce(ctx); err != nil && r.onError != nil {
				r.onError(err)
			}
		}
	}
}

func (r *APYRelayer) RunOnce(ctx context.Context) error {
	grouped, collectErr := r.collectGroupedQuotes(ctx)
	now := r.now().UTC()

	aggregated, aggErr := r.aggregateGroupedQuotes(grouped)

	var updateErrs []error
	for protocolID, quote := range aggregated {
		// Deviation check against last applied APY (if any)
		r.mu.Lock()
		last := r.lastAPY[protocolID]
		r.mu.Unlock()
		if err := r.checkDeviation(float64(quote.APYBPS), float64(last)); err != nil {
			if r.onError != nil {
				r.onError(fmt.Errorf("APY deviation for %s: %w", protocolID, err))
			}
			continue
		}

		if err := r.updater.UpdateAPY(ctx, r.registryID, protocolID, quote.APYBPS); err != nil {
			updateErrs = append(updateErrs, fmt.Errorf("%s update failed for %s: %w", quote.Source, protocolID, err))
			continue
		}

		updatedAt := quote.UpdatedAt.UTC()
		if updatedAt.IsZero() {
			updatedAt = now
		}
		r.markUpdatedWithAPY(protocolID, updatedAt, quote.APYBPS)
	}

	r.checkStaleness(now)

	// Combine errors: prefer reporting per-source collection errors as well
	if len(updateErrs) > 0 {
		updateErrs = append(updateErrs, aggErr)
		if collectErr != nil {
			updateErrs = append(updateErrs, collectErr)
		}
		return errors.Join(updateErrs...)
	}

	if aggErr != nil {
		return aggErr
	}
	return collectErr
}

func (r *APYRelayer) markUpdated(protocolID string, updatedAt time.Time) {
	// convenience wrapper for callers without an APY value
	r.markUpdatedWithAPY(protocolID, updatedAt, 0)
}

// markUpdated stores last update time and last-applied APY (if > 0).
func (r *APYRelayer) markUpdatedWithAPY(protocolID string, updatedAt time.Time, apy uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastUpdated[protocolID] = updatedAt
	r.staleAlerted[protocolID] = false
	if apy > 0 {
		r.lastAPY[protocolID] = apy
	}
}

// (removed overloaded markUpdated wrapper)

func (r *APYRelayer) checkDeviation(newAPY, lastAPY float64) error {
	if lastAPY == 0 {
		return nil
	}
	deviation := math.Abs(newAPY-lastAPY) / lastAPY
	if deviation > r.maxDeviationPct {
		return fmt.Errorf("APY deviation %.2f%% exceeds max %.2f%% — requires manual confirmation", deviation*100, r.maxDeviationPct*100)
	}
	return nil
}

func (r *APYRelayer) collectQuotes(ctx context.Context) (map[string]APYQuote, error) {
	merged := make(map[string]APYQuote)
	var errs []error

	for _, source := range r.sources {
		quotes, err := source.Fetch(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s source fetch failed: %w", source.Name(), err))
			continue
		}

		for _, quote := range quotes {
			if strings.TrimSpace(quote.ProtocolID) == "" {
				continue
			}
			if quote.Source == "" {
				quote.Source = source.Name()
			}
			if quote.UpdatedAt.IsZero() {
				quote.UpdatedAt = r.now().UTC()
			}

			existing, exists := merged[quote.ProtocolID]
			if !exists || quote.UpdatedAt.After(existing.UpdatedAt) {
				merged[quote.ProtocolID] = quote
			}
		}
	}

	if len(errs) == 0 {
		return merged, nil
	}
	return merged, errors.Join(errs...)
}

func (r *APYRelayer) checkStaleness(now time.Time) {
	if r.onStale == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, source := range r.sources {
		for _, protocolID := range source.ProtocolIDs() {
			last, ok := r.lastUpdated[protocolID]
			if !ok {
				last = time.Time{}
			}

			age := now.Sub(last)
			if last.IsZero() {
				age = now.Sub(time.Time{})
			}

			isStale := last.IsZero() || age > r.stalenessThreshold
			if isStale && !r.staleAlerted[protocolID] {
				r.onStale(StaleAPYAlert{
					ProtocolID:    protocolID,
					LastUpdatedAt: last,
					Age:           age,
					Threshold:     r.stalenessThreshold,
				})
				r.staleAlerted[protocolID] = true
			}
			if !isStale {
				r.staleAlerted[protocolID] = false
			}
		}
	}
}

type YieldRegistryUpdater struct {
	Invoker      *ContractInvoker
	OperatorAddr string
}

func (u *YieldRegistryUpdater) UpdateAPY(
	ctx context.Context,
	registryID string,
	protocolID string,
	apyBPS uint32,
) error {
	if u == nil || u.Invoker == nil {
		return errors.New("yield registry updater is not configured")
	}

	_, err := u.Invoker.InvokeContract(
		ctx,
		registryID,
		"update_apy",
		[]interface{}{u.OperatorAddr, protocolID, int64(apyBPS)},
	)
	return err
}

type DeFiLlamaSource struct {
	client         *DeFiLlamaClient
	protocolToPool map[string]string
}

func NewDeFiLlamaSource(client *DeFiLlamaClient, protocolToPool map[string]string) (*DeFiLlamaSource, error) {
	if client == nil {
		return nil, errors.New("defillama client is required")
	}
	if len(protocolToPool) == 0 {
		return nil, errors.New("protocol to pool mapping is required")
	}
	return &DeFiLlamaSource{
		client:         client,
		protocolToPool: protocolToPool,
	}, nil
}

func (s *DeFiLlamaSource) Name() string {
	return "defillama"
}

func (s *DeFiLlamaSource) ProtocolIDs() []string {
	out := make([]string, 0, len(s.protocolToPool))
	for protocolID := range s.protocolToPool {
		out = append(out, protocolID)
	}
	return out
}

func (s *DeFiLlamaSource) Fetch(ctx context.Context) ([]APYQuote, error) {
	poolIDs := make([]string, 0, len(s.protocolToPool))
	for _, poolID := range s.protocolToPool {
		poolIDs = append(poolIDs, poolID)
	}

	snapshots, err := s.client.APYByPool(ctx, poolIDs)
	if err != nil {
		return nil, err
	}

	out := make([]APYQuote, 0, len(s.protocolToPool))
	for protocolID, poolID := range s.protocolToPool {
		snapshot, ok := snapshots[poolID]
		if !ok {
			return nil, fmt.Errorf("missing defillama snapshot for pool %q", poolID)
		}
		out = append(out, APYQuote{
			ProtocolID: protocolID,
			APYBPS:     snapshot.APYBPS,
			UpdatedAt:  snapshot.UpdatedAt,
			Source:     s.Name(),
		})
	}
	return out, nil
}

type ProtocolRPCClient struct {
	baseURL    string
	httpClient *http.Client
}

type protocolAPYResponse struct {
	APYBPS    uint32 `json:"apy_bps"`
	UpdatedAt string `json:"updated_at"`
}

func NewProtocolRPCClient(httpClient *http.Client, baseURL string) (*ProtocolRPCClient, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return nil, errors.New("protocol RPC base URL is required")
	}

	parsed, err := url.ParseRequestURI(trimmed)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("invalid protocol RPC base URL %q: must be a valid absolute URL", trimmed)
	}
	if parsed.Scheme != "https" {
		return nil, fmt.Errorf("protocol RPC base URL must use HTTPS, got scheme %q", parsed.Scheme)
	}

	if httpClient == nil {
		httpClient = &http.Client{
			Timeout:   10 * time.Second,
			Transport: ssrfSafeTransport(),
		}
	}
	return &ProtocolRPCClient{
		baseURL:    trimmed,
		httpClient: httpClient,
	}, nil
}

func (c *ProtocolRPCClient) FetchProtocolAPY(ctx context.Context, protocolID string) (APYSnapshot, error) {
	if !validProtocolID.MatchString(protocolID) {
		return APYSnapshot{}, fmt.Errorf("invalid protocolID format: %q — must match [a-zA-Z0-9_-]{1,64}", protocolID)
	}

	apiURL := fmt.Sprintf("%s/v1/protocols/%s/apy", c.baseURL, url.PathEscape(protocolID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return APYSnapshot{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return APYSnapshot{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return APYSnapshot{}, fmt.Errorf("protocol RPC returned status %d for %q", resp.StatusCode, protocolID)
	}

	var payload protocolAPYResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return APYSnapshot{}, fmt.Errorf("decode protocol APY response: %w", err)
	}

	updated := time.Now().UTC()
	if strings.TrimSpace(payload.UpdatedAt) != "" {
		if ts, parseErr := time.Parse(time.RFC3339, payload.UpdatedAt); parseErr == nil {
			updated = ts.UTC()
		}
	}

	return APYSnapshot{
		APYBPS:    payload.APYBPS,
		UpdatedAt: updated,
	}, nil
}

type ProtocolRPCSource struct {
	client      *ProtocolRPCClient
	protocolIDs []string
}

func NewProtocolRPCSource(client *ProtocolRPCClient, protocolIDs []string) (*ProtocolRPCSource, error) {
	if client == nil {
		return nil, errors.New("protocol RPC client is required")
	}
	if len(protocolIDs) == 0 {
		return nil, errors.New("protocol IDs are required")
	}
	return &ProtocolRPCSource{
		client:      client,
		protocolIDs: protocolIDs,
	}, nil
}

func (s *ProtocolRPCSource) Name() string {
	return "protocol_rpc"
}

func (s *ProtocolRPCSource) ProtocolIDs() []string {
	out := make([]string, len(s.protocolIDs))
	copy(out, s.protocolIDs)
	return out
}

func (s *ProtocolRPCSource) Fetch(ctx context.Context) ([]APYQuote, error) {
	out := make([]APYQuote, 0, len(s.protocolIDs))
	var errs []error

	for _, protocolID := range s.protocolIDs {
		snapshot, err := s.client.FetchProtocolAPY(ctx, protocolID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, APYQuote{
			ProtocolID: protocolID,
			APYBPS:     snapshot.APYBPS,
			UpdatedAt:  snapshot.UpdatedAt,
			Source:     s.Name(),
		})
	}

	if len(errs) > 0 {
		return out, errors.Join(errs...)
	}
	return out, nil
}
