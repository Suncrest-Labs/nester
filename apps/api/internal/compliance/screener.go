package compliance

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stellar/go/strkey"
)

const (
	// DefaultRefreshInterval is how often the OFAC SDN feed is refreshed.
	DefaultRefreshInterval = 24 * time.Hour

	// DefaultMaxStaleness is the maximum age of the last successful OFAC SDN
	// refresh before staleness alerting fires.
	DefaultMaxStaleness = 36 * time.Hour

	// GenericBlockedMessage is the user-facing error message returned when a
	// wallet address is blocked by sanctions screening. It intentionally avoids
	// tipping off the caller about specific watchlists.
	GenericBlockedMessage = "request cannot be processed for this wallet address"
)

// ErrAddressRestricted is returned when a wallet address hits the sanctions
// screening policy.
var ErrAddressRestricted = errors.New(GenericBlockedMessage)

var (
	// SanctionsHitsTotal counts blocked wallet addresses at auth and deposit.
	SanctionsHitsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "nester",
		Subsystem: "compliance",
		Name:      "sanctions_hits_total",
		Help:      "Total wallet addresses blocked by sanctions screening.",
	})

	// SanctionsRefreshStaleAlertsTotal counts staleness alerts emitted when the
	// OFAC SDN refresh job exceeds its maximum age budget.
	SanctionsRefreshStaleAlertsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "nester",
		Subsystem: "compliance",
		Name:      "sanctions_refresh_stale_alerts_total",
		Help:      "Total staleness alerts raised by the OFAC SDN sanctions refresh job.",
	})
)

// Decision represents the outcome of screening a Stellar wallet address.
type Decision struct {
	Allowed    bool
	Blocked    bool
	Source     string
	Reason     string
	ScreenedAt time.Time
}

// Screener screens a Stellar wallet address against sanctions controls.
type Screener interface {
	ScreenAddress(ctx context.Context, walletAddress string) (Decision, error)
}

// AuditHook records a compliance hit to the durable audit trail.
type AuditHook func(ctx context.Context, walletAddress, checkPoint, source string)

// Provider is an optional external commercial screening provider.
type Provider interface {
	CheckAddress(ctx context.Context, walletAddress string) (blocked bool, reason string, err error)
}

// Config configures the pluggable sanctions screener.
type Config struct {
	// Enabled controls whether screening is active. Configurable per network
	// (enabled on mainnet, optional on testnet).
	Enabled bool

	// StaticAddresses is the seed list of sanctioned Stellar addresses.
	StaticAddresses []string

	// OFACSDNURL is the URL of the OFAC SDN XML feed used by the daily refresh job.
	OFACSDNURL string

	// HTTPClient is used to fetch the OFAC SDN XML feed.
	HTTPClient *http.Client

	// Provider is an optional secondary commercial screening provider.
	Provider Provider

	// MaxStaleness is the maximum allowed age of the last successful refresh
	// before CheckStaleness raises an alert. Defaults to DefaultMaxStaleness.
	MaxStaleness time.Duration

	// Logger is used for structured compliance and staleness alerts.
	Logger *slog.Logger

	// OnAuditHit is invoked whenever an address hits the static/OFAC list or
	// the external provider.
	OnAuditHit AuditHook

	// Now overrides time.Now in tests.
	Now func() time.Time
}

// StandardScreener implements Screener using a static + OFAC SDN refreshed
// address set and an optional commercial provider.
type StandardScreener struct {
	enabled      bool
	ofacURL      string
	httpClient   *http.Client
	provider     Provider
	maxStaleness time.Duration
	logger       *slog.Logger
	onAuditHit   AuditHook
	now          func() time.Time

	mu            sync.RWMutex
	staticSet     map[string]struct{}
	ofacSet       map[string]struct{}
	lastRefreshed time.Time

	staleAlertsCount atomic.Uint64
	hitsCount        atomic.Uint64
}

// NewStandardScreener constructs a StandardScreener from Config.
func NewStandardScreener(cfg Config) *StandardScreener {
	staticSet := make(map[string]struct{}, len(cfg.StaticAddresses))
	for _, addr := range cfg.StaticAddresses {
		norm := normalizeAddress(addr)
		if norm != "" {
			staticSet[norm] = struct{}{}
		}
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	maxStaleness := cfg.MaxStaleness
	if maxStaleness <= 0 {
		maxStaleness = DefaultMaxStaleness
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	return &StandardScreener{
		enabled:      cfg.Enabled,
		ofacURL:      strings.TrimSpace(cfg.OFACSDNURL),
		httpClient:   httpClient,
		provider:     cfg.Provider,
		maxStaleness: maxStaleness,
		logger:       logger,
		onAuditHit:   cfg.OnAuditHit,
		now:          nowFn,
		staticSet:    staticSet,
		ofacSet:      make(map[string]struct{}),
	}
}

func normalizeAddress(addr string) string {
	return strings.ToUpper(strings.TrimSpace(addr))
}

// ScreenAddress checks a Stellar wallet address against the static list, the
// refreshed OFAC SDN list, and the optional external provider.
func (s *StandardScreener) ScreenAddress(ctx context.Context, walletAddress string) (Decision, error) {
	now := s.now()
	if s == nil || !s.enabled {
		return Decision{Allowed: true, ScreenedAt: now}, nil
	}

	norm := normalizeAddress(walletAddress)
	if norm == "" {
		return Decision{}, fmt.Errorf("wallet address is required")
	}

	s.mu.RLock()
	_, inStatic := s.staticSet[norm]
	_, inOFAC := s.ofacSet[norm]
	s.mu.RUnlock()

	if inStatic {
		s.recordHit(ctx, norm, "static_sanctions_list")
		return Decision{
			Allowed:    false,
			Blocked:    true,
			Source:     "static_sanctions_list",
			Reason:     GenericBlockedMessage,
			ScreenedAt: now,
		}, ErrAddressRestricted
	}

	if inOFAC {
		s.recordHit(ctx, norm, "ofac_sdn")
		return Decision{
			Allowed:    false,
			Blocked:    true,
			Source:     "ofac_sdn",
			Reason:     GenericBlockedMessage,
			ScreenedAt: now,
		}, ErrAddressRestricted
	}

	if s.provider != nil {
		blocked, _, err := s.provider.CheckAddress(ctx, norm)
		if err != nil {
			return Decision{}, fmt.Errorf("screening provider check failed: %w", err)
		}
		if blocked {
			s.recordHit(ctx, norm, "external_provider")
			return Decision{
				Allowed:    false,
				Blocked:    true,
				Source:     "external_provider",
				Reason:     GenericBlockedMessage,
				ScreenedAt: now,
			}, ErrAddressRestricted
		}
	}

	return Decision{
		Allowed:    true,
		Blocked:    false,
		Source:     "clean",
		ScreenedAt: now,
	}, nil
}

func (s *StandardScreener) recordHit(ctx context.Context, walletAddress, source string) {
	SanctionsHitsTotal.Inc()
	s.hitsCount.Add(1)
	s.logger.Error(
		"ALERT: sanctions screening blocked wallet address",
		"alert", "compliance_sanctions_hit",
		"wallet_address", walletAddress,
		"source", source,
	)
	if s.onAuditHit != nil {
		s.onAuditHit(ctx, walletAddress, "sanctions_screen", source)
	}
}

// HitsCount returns the number of blocked screening hits observed by this instance.
func (s *StandardScreener) HitsCount() uint64 {
	return s.hitsCount.Load()
}

// ScreenWalletAddress implements service.WalletSanctionsScreener by returning
// ErrAddressRestricted when ScreenAddress blocks the address.
func (s *StandardScreener) ScreenWalletAddress(ctx context.Context, walletAddress string) error {
	decision, err := s.ScreenAddress(ctx, walletAddress)
	if err != nil {
		return err
	}
	if decision.Blocked {
		return ErrAddressRestricted
	}
	return nil
}

// StaleAlertsCount returns the number of staleness alerts raised by this instance.
func (s *StandardScreener) StaleAlertsCount() uint64 {
	return s.staleAlertsCount.Load()
}

// RefreshFromOFACReader parses an OFAC SDN XML stream, extracts all valid
// Stellar digital currency addresses, and atomically replaces the dynamic OFAC
// set while recording the refresh timestamp.
func (s *StandardScreener) RefreshFromOFACReader(r io.Reader) (int, error) {
	addresses, err := ParseOFACSDNXML(r)
	if err != nil {
		return 0, err
	}
	next := make(map[string]struct{}, len(addresses))
	for _, addr := range addresses {
		next[addr] = struct{}{}
	}

	s.mu.Lock()
	s.ofacSet = next
	s.lastRefreshed = s.now()
	s.mu.Unlock()

	return len(next), nil
}

// RefreshFromOFAC fetches the configured OFAC SDN XML URL and updates the
// dynamic address set.
func (s *StandardScreener) RefreshFromOFAC(ctx context.Context) (int, error) {
	if s.ofacURL == "" {
		return 0, fmt.Errorf("ofac sdn url is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.ofacURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("ofac sdn feed returned status %d", resp.StatusCode)
	}
	return s.RefreshFromOFACReader(resp.Body)
}

// CheckStaleness verifies that the OFAC SDN sanctions list was refreshed within
// MaxStaleness. When stale, it increments the staleness metric and logs an alert.
func (s *StandardScreener) CheckStaleness() bool {
	s.mu.RLock()
	last := s.lastRefreshed
	s.mu.RUnlock()

	now := s.now()
	if last.IsZero() || now.Sub(last) > s.maxStaleness {
		SanctionsRefreshStaleAlertsTotal.Inc()
		s.staleAlertsCount.Add(1)
		var age time.Duration
		if !last.IsZero() {
			age = now.Sub(last)
		}
		s.logger.Error(
			"ALERT: OFAC SDN sanctions list is stale",
			"alert", "sanctions_list_stale",
			"last_refreshed", last,
			"age_seconds", age.Seconds(),
			"max_staleness_seconds", s.maxStaleness.Seconds(),
		)
		return true
	}
	return false
}

// ParseOFACSDNXML extracts valid Stellar addresses (G... or C... strkeys) from
// an OFAC SDN XML document (supporting both `<id>` entries with
// `Digital Currency Address - XLM` and generic `<idNumber>` / text tokens).
func ParseOFACSDNXML(r io.Reader) ([]string, error) {
	decoder := xml.NewDecoder(r)
	seen := make(map[string]struct{})
	var out []string

	var inID bool
	var currentIDType string
	var currentIDNumber string

	for {
		tok, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse ofac sdn xml: %w", err)
		}

		switch se := tok.(type) {
		case xml.StartElement:
			switch strings.ToLower(se.Name.Local) {
			case "id", "feature":
				inID = true
				currentIDType = ""
				currentIDNumber = ""
			case "idtype":
				var val string
				if err := decoder.DecodeElement(&val, &se); err == nil {
					currentIDType = strings.TrimSpace(val)
				}
			case "idnumber", "value":
				var val string
				if err := decoder.DecodeElement(&val, &se); err == nil {
					currentIDNumber = strings.TrimSpace(val)
					if isValidStellarAddress(currentIDNumber) {
						norm := normalizeAddress(currentIDNumber)
						if _, exists := seen[norm]; !exists {
							seen[norm] = struct{}{}
							out = append(out, norm)
						}
					}
				}
			}
		case xml.EndElement:
			switch strings.ToLower(se.Name.Local) {
			case "id", "feature":
				if inID && currentIDNumber != "" && isValidStellarAddress(currentIDNumber) {
					_ = currentIDType
					norm := normalizeAddress(currentIDNumber)
					if _, exists := seen[norm]; !exists {
						seen[norm] = struct{}{}
						out = append(out, norm)
					}
				}
				inID = false
			}
		}
	}

	return out, nil
}

func isValidStellarAddress(candidate string) bool {
	candidate = strings.TrimSpace(candidate)
	if len(candidate) != 56 {
		return false
	}
	if _, err := strkey.Decode(strkey.VersionByteAccountID, candidate); err == nil {
		return true
	}
	if _, err := strkey.Decode(strkey.VersionByteContract, candidate); err == nil {
		return true
	}
	return false
}
