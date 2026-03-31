package stellar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const defaultAPYStalenessThreshold = time.Hour

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
	}, nil
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
	quotes, collectErr := r.collectQuotes(ctx)
	now := r.now().UTC()

	var updateErrs []error
	for protocolID, quote := range quotes {
		if err := r.updater.UpdateAPY(ctx, r.registryID, protocolID, quote.APYBPS); err != nil {
			updateErrs = append(updateErrs, fmt.Errorf("%s update failed for %s: %w", quote.Source, protocolID, err))
			continue
		}

		updatedAt := quote.UpdatedAt.UTC()
		if updatedAt.IsZero() {
			updatedAt = now
		}
		r.markUpdated(protocolID, updatedAt)
	}

	r.checkStaleness(now)

	if len(updateErrs) > 0 && collectErr != nil {
		updateErrs = append(updateErrs, collectErr)
		return errors.Join(updateErrs...)
	}
	if len(updateErrs) > 0 {
		return errors.Join(updateErrs...)
	}
	return collectErr
}

func (r *APYRelayer) markUpdated(protocolID string, updatedAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastUpdated[protocolID] = updatedAt
	r.staleAlerted[protocolID] = false
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

func NewProtocolRPCClient(httpClient *http.Client, baseURL string) *ProtocolRPCClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &ProtocolRPCClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: httpClient,
	}
}

func (c *ProtocolRPCClient) FetchProtocolAPY(ctx context.Context, protocolID string) (APYSnapshot, error) {
	url := fmt.Sprintf("%s/v1/protocols/%s/apy", c.baseURL, protocolID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return APYSnapshot{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return APYSnapshot{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return APYSnapshot{}, fmt.Errorf("protocol RPC returned status %d for %s", resp.StatusCode, protocolID)
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
