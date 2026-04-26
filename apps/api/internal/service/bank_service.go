package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/bank"
)

const bankListCacheTTL = 24 * time.Hour

// cachedBankList holds a snapshot of the bank list and its expiry time.
type cachedBankList struct {
	banks     []bank.Bank
	expiresAt time.Time
}

// BankService handles bank-list caching and account name resolution with
// automatic provider fallback (Paystack → Flutterwave).
type BankService struct {
	primary   bank.BankResolver
	fallback  bank.BankResolver
	cacheMu   sync.RWMutex
	listCache map[string]cachedBankList // keyed by country code
}

// NewBankService creates a BankService.  primary is tried first on every
// call; fallback is used automatically when primary returns a non-domain error.
func NewBankService(primary, fallback bank.BankResolver) *BankService {
	return &BankService{
		primary:   primary,
		fallback:  fallback,
		listCache: make(map[string]cachedBankList),
	}
}

// ListBanks returns the bank list for country, using a 24-hour in-memory
// cache.  The cache is populated from primary; if primary fails the fallback
// is tried.  Cached results are served even when both providers are down.
func (s *BankService) ListBanks(ctx context.Context, country string) ([]bank.Bank, error) {
	country = strings.ToUpper(strings.TrimSpace(country))
	if !bank.SupportedCountries[country] {
		return nil, bank.ErrInvalidCountry
	}

	// Fast path: serve from cache if still fresh.
	s.cacheMu.RLock()
	entry, ok := s.listCache[country]
	s.cacheMu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.banks, nil
	}

	// Slow path: fetch from provider.
	banks, err := s.primary.ListBanks(ctx, country)
	if err != nil {
		// Primary failed — try fallback.
		banks, err = s.fallback.ListBanks(ctx, country)
		if err != nil {
			// Both failed — return stale cache if available rather than an error.
			if ok {
				return entry.banks, nil
			}
			return nil, bank.ErrProviderUnavailable
		}
	}

	// Update cache.
	s.cacheMu.Lock()
	s.listCache[country] = cachedBankList{
		banks:     banks,
		expiresAt: time.Now().Add(bankListCacheTTL),
	}
	s.cacheMu.Unlock()

	return banks, nil
}

// ResolveAccount performs NUBAN name-enquiry.  Paystack is tried first;
// Flutterwave is the automatic fallback on any non-domain error.
//
// PII note: account numbers and account names are never included in log
// fields — callers must not log the inputs or outputs of this function.
func (s *BankService) ResolveAccount(ctx context.Context, accountNumber, bankCode, country string) (*bank.AccountInfo, error) {
	// Validate inputs before hitting any provider.
	accountNumber = strings.TrimSpace(accountNumber)
	bankCode = strings.TrimSpace(bankCode)
	country = strings.ToUpper(strings.TrimSpace(country))

	if len(accountNumber) != 10 || !isDigits(accountNumber) {
		return nil, bank.ErrInvalidAccountNumber
	}
	if bankCode == "" {
		return nil, bank.ErrInvalidBankCode
	}
	if !bank.SupportedCountries[country] {
		return nil, bank.ErrInvalidCountry
	}

	info, err := s.primary.ResolveAccount(ctx, accountNumber, bankCode)
	if err != nil {
		// If primary explicitly says account not found, don't try fallback —
		// the account genuinely doesn't exist.
		if errors.Is(err, bank.ErrAccountNotFound) {
			return nil, bank.ErrAccountNotFound
		}
		// Primary is unavailable — try fallback.
		info, err = s.fallback.ResolveAccount(ctx, accountNumber, bankCode)
		if err != nil {
			if errors.Is(err, bank.ErrAccountNotFound) {
				return nil, bank.ErrAccountNotFound
			}
			return nil, bank.ErrProviderUnavailable
		}
	}

	// Enrich with bank name from cached list.
	info.BankName = s.lookupBankName(ctx, bankCode, country)

	return info, nil
}

// lookupBankName returns the display name for bankCode from the cached list.
// Returns an empty string on any error — this is best-effort enrichment.
func (s *BankService) lookupBankName(ctx context.Context, bankCode, country string) string {
	s.cacheMu.RLock()
	entry, ok := s.listCache[country]
	s.cacheMu.RUnlock()

	if !ok {
		// Cache miss — try to populate it but don't block the resolution response.
		banks, err := s.ListBanks(ctx, country)
		if err != nil {
			return ""
		}
		for _, b := range banks {
			if b.Code == bankCode {
				return b.Name
			}
		}
		return ""
	}

	for _, b := range entry.banks {
		if b.Code == bankCode {
			return b.Name
		}
	}
	return ""
}

// isDigits reports whether s contains only ASCII digit characters.
func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}
