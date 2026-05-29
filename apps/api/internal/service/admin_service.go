package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	admindomain "github.com/suncrestlabs/nester/apps/api/internal/domain/admin"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

var (
	ErrInvalidAdminInput = errors.New("invalid admin input")
)

const (
	dashboardCacheTTL   = 2 * time.Minute
	apyDropAlertThreshold = 0.20
)

type VaultChainInvoker interface {
	PauseVault(ctx context.Context, contractAddress string) error
	UnpauseVault(ctx context.Context, contractAddress string) error
}

// NoopVaultChainInvoker is the default invoker used when no on-chain
// integration is configured in-process.
type NoopVaultChainInvoker struct{}

func (NoopVaultChainInvoker) PauseVault(_ context.Context, _ string) error   { return nil }
func (NoopVaultChainInvoker) UnpauseVault(_ context.Context, _ string) error { return nil }

type AdminService struct {
	repository            admindomain.Repository
	chainInvoker          VaultChainInvoker
	httpClient            *http.Client
	stellarHorizonURL     string
	settlementProviderURL string
	startedAt             time.Time

	dashboardCache   *admindomain.VaultHealthDashboard
	dashboardCacheAt time.Time
	dashboardCacheMu sync.RWMutex
}

func NewAdminService(
	repository admindomain.Repository,
	chainInvoker VaultChainInvoker,
	stellarHorizonURL string,
	settlementProviderURL string,
) *AdminService {
	if chainInvoker == nil {
		chainInvoker = NoopVaultChainInvoker{}
	}

	return &AdminService{
		repository:            repository,
		chainInvoker:          chainInvoker,
		httpClient:            &http.Client{Timeout: 5 * time.Second},
		stellarHorizonURL:     stellarHorizonURL,
		settlementProviderURL: settlementProviderURL,
		startedAt:             time.Now().UTC(),
	}
}

func (s *AdminService) GetDashboard(ctx context.Context) (admindomain.VaultHealthDashboard, error) {
	s.dashboardCacheMu.RLock()
	if s.dashboardCache != nil && time.Since(s.dashboardCacheAt) < dashboardCacheTTL {
		cached := *s.dashboardCache
		s.dashboardCacheMu.RUnlock()
		return cached, nil
	}
	s.dashboardCacheMu.RUnlock()

	data, err := s.repository.GetVaultHealthDashboard(ctx)
	if err != nil {
		return admindomain.VaultHealthDashboard{}, err
	}

	result := buildVaultHealthDashboard(data)

	s.dashboardCacheMu.Lock()
	s.dashboardCache = &result
	s.dashboardCacheAt = time.Now().UTC()
	s.dashboardCacheMu.Unlock()

	return result, nil
}

func buildVaultHealthDashboard(data admindomain.VaultHealthDashboardData) admindomain.VaultHealthDashboard {
	vaults := make([]admindomain.VaultHealthEntry, 0, len(data.Vaults))
	systemAlerts := make([]admindomain.SystemAlert, 0)

	for _, row := range data.Vaults {
		entry := admindomain.VaultHealthEntry{
			ID:                  row.ID,
			Name:                row.Name,
			TVLUSDC:             row.TVL.StringFixed(2),
			APY7d:               formatAPY(row.APY7d),
			Depositors:          row.Depositors,
			PendingTransactions: row.PendingTransactions,
			Status:              mapVaultHealthStatus(row.Status),
			Alerts:              []admindomain.VaultAlert{},
		}

		if row.LastRebalanceAt != nil {
			formatted := row.LastRebalanceAt.UTC().Format(time.RFC3339)
			entry.LastRebalanceAt = &formatted
		}

		if alert := apyDropAlert(row.Name, row.APY7d, row.APY7d24hAgo); alert != nil {
			systemAlerts = append(systemAlerts, *alert)
		}

		vaults = append(vaults, entry)
	}

	return admindomain.VaultHealthDashboard{
		TotalTVLUSDC:    data.TotalTVL.StringFixed(2),
		TotalDepositors: data.TotalDepositors,
		Vaults:          vaults,
		SystemAlerts:    systemAlerts,
	}
}

func formatAPY(apy *decimal.Decimal) string {
	if apy == nil {
		return "0.00"
	}
	return apy.StringFixed(2)
}

func mapVaultHealthStatus(status vault.VaultStatus) string {
	switch status {
	case vault.StatusActive:
		return "healthy"
	case vault.StatusPaused:
		return "paused"
	default:
		return string(status)
	}
}

func apyDropAlert(name string, current, previous *decimal.Decimal) *admindomain.SystemAlert {
	if current == nil || previous == nil || previous.IsZero() {
		return nil
	}

	drop := previous.Sub(*current).Div(*previous)
	if drop.LessThanOrEqual(decimal.NewFromFloat(apyDropAlertThreshold)) {
		return nil
	}

	pctDrop := drop.Mul(decimal.NewFromInt(100)).Round(0)
	return &admindomain.SystemAlert{
		Severity: "warning",
		Message: fmt.Sprintf(
			"%s vault APY dropped %s%% in 24h",
			name,
			pctDrop.StringFixed(0),
		),
	}
}

func (s *AdminService) ListVaults(
	ctx context.Context,
	filter admindomain.VaultListFilter,
) ([]admindomain.VaultSummary, int, error) {
	return s.repository.ListVaults(ctx, filter)
}

func (s *AdminService) GetVaultDetail(ctx context.Context, id uuid.UUID) (admindomain.VaultDetail, error) {
	if id == uuid.Nil {
		return admindomain.VaultDetail{}, ErrInvalidAdminInput
	}
	return s.repository.GetVaultDetail(ctx, id)
}

func (s *AdminService) PauseVault(ctx context.Context, id uuid.UUID) (admindomain.VaultDetail, error) {
	if id == uuid.Nil {
		return admindomain.VaultDetail{}, ErrInvalidAdminInput
	}

	current, err := s.repository.GetVaultDetail(ctx, id)
	if err != nil {
		return admindomain.VaultDetail{}, err
	}

	if err := s.chainInvoker.PauseVault(ctx, current.ContractAddress); err != nil {
		return admindomain.VaultDetail{}, fmt.Errorf("on-chain pause failed: %w", err)
	}

	return s.repository.UpdateVaultStatus(ctx, id, vault.StatusPaused)
}

func (s *AdminService) UnpauseVault(ctx context.Context, id uuid.UUID) (admindomain.VaultDetail, error) {
	if id == uuid.Nil {
		return admindomain.VaultDetail{}, ErrInvalidAdminInput
	}

	current, err := s.repository.GetVaultDetail(ctx, id)
	if err != nil {
		return admindomain.VaultDetail{}, err
	}

	if err := s.chainInvoker.UnpauseVault(ctx, current.ContractAddress); err != nil {
		return admindomain.VaultDetail{}, fmt.Errorf("on-chain unpause failed: %w", err)
	}

	return s.repository.UpdateVaultStatus(ctx, id, vault.StatusActive)
}

func (s *AdminService) ListSettlements(
	ctx context.Context,
	filter admindomain.SettlementListFilter,
) ([]admindomain.SettlementSummary, int, error) {
	return s.repository.ListSettlements(ctx, filter)
}

func (s *AdminService) ListUsers(
	ctx context.Context,
	filter admindomain.UserListFilter,
) ([]admindomain.UserSummary, int, error) {
	return s.repository.ListUsers(ctx, filter)
}

func (s *AdminService) GetDetailedHealth(ctx context.Context) (admindomain.DetailedHealth, error) {
	database := s.checkDatabase(ctx)
	stellar := s.checkHTTPDependency(ctx, s.stellarHorizonURL, "stellar horizon")
	settlement := s.checkHTTPDependency(ctx, s.settlementProviderURL, "settlement provider")
	indexer := s.checkEventIndexer(ctx)

	return admindomain.DetailedHealth{
		Database:           database,
		StellarRPC:         stellar,
		SettlementProvider: settlement,
		EventIndexer:       indexer,
		DiskUsage:          diskUsage(),
		Uptime:             time.Since(s.startedAt).Round(time.Second).String(),
	}, nil
}

func (s *AdminService) checkDatabase(ctx context.Context) admindomain.HealthStatus {
	checkedAt := time.Now().UTC()
	latencyMS, err := s.repository.DatabaseHealth(ctx)
	if err != nil {
		return admindomain.HealthStatus{
			Status:        "unhealthy",
			Message:       err.Error(),
			LastCheckedAt: checkedAt,
		}
	}
	return admindomain.HealthStatus{
		Status:        "healthy",
		LatencyMS:     latencyMS,
		LastCheckedAt: checkedAt,
	}
}

func (s *AdminService) checkHTTPDependency(
	ctx context.Context,
	url string,
	name string,
) admindomain.HealthStatus {
	checkedAt := time.Now().UTC()
	if url == "" {
		return admindomain.HealthStatus{
			Status:        "unknown",
			Message:       name + " URL not configured",
			LastCheckedAt: checkedAt,
		}
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return admindomain.HealthStatus{
			Status:        "unhealthy",
			Message:       err.Error(),
			LastCheckedAt: checkedAt,
		}
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return admindomain.HealthStatus{
			Status:        "unhealthy",
			Message:       err.Error(),
			LastCheckedAt: checkedAt,
		}
	}
	defer resp.Body.Close()

	latency := time.Since(start).Milliseconds()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return admindomain.HealthStatus{
			Status:        "healthy",
			LatencyMS:     latency,
			LastCheckedAt: checkedAt,
		}
	}

	return admindomain.HealthStatus{
		Status:        "degraded",
		LatencyMS:     latency,
		Message:       fmt.Sprintf("%s returned status %d", name, resp.StatusCode),
		LastCheckedAt: checkedAt,
	}
}

func (s *AdminService) checkEventIndexer(ctx context.Context) admindomain.HealthStatus {
	checkedAt := time.Now().UTC()
	lastEventAt, err := s.repository.GetLastEventIndexedAt(ctx)
	if err != nil {
		return admindomain.HealthStatus{
			Status:        "unhealthy",
			Message:       err.Error(),
			LastCheckedAt: checkedAt,
		}
	}
	if lastEventAt == nil {
		return admindomain.HealthStatus{
			Status:        "degraded",
			Message:       "no indexed events yet",
			LastCheckedAt: checkedAt,
		}
	}

	lag := int64(time.Since(*lastEventAt).Seconds())
	status := "healthy"
	msg := ""
	if lag > 3600 {
		status = "degraded"
		msg = "event indexer lag exceeds 1 hour"
	}

	return admindomain.HealthStatus{
		Status:        status,
		Message:       msg,
		LastCheckedAt: checkedAt,
		LastEventAt:   lastEventAt,
		LagSeconds:    lag,
	}

}
