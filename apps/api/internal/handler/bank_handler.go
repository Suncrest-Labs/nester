package handler

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/bank"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// resolveRateLimiter enforces 10 name-enquiry calls per IP per minute.
// Each call costs money (Paystack/Flutterwave billing), so this is a hard cap.
var resolveRateLimiter = newIPLimiter(10, time.Minute)

// ipLimiter is a simple per-IP sliding-window counter.
type ipLimiter struct {
	mu      sync.Mutex
	buckets map[string]*ipBucket
	limit   int
	window  time.Duration
}

type ipBucket struct {
	count     int
	windowEnd time.Time
}

func newIPLimiter(limit int, window time.Duration) *ipLimiter {
	return &ipLimiter{buckets: make(map[string]*ipBucket), limit: limit, window: window}
}

func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.buckets[ip]
	if !ok || now.After(b.windowEnd) {
		l.buckets[ip] = &ipBucket{count: 1, windowEnd: now.Add(l.window)}
		return true
	}
	if b.count >= l.limit {
		return false
	}
	b.count++
	return true
}

// BankHandler exposes endpoints for bank listing and account name resolution.
type BankHandler struct {
	service *service.BankService
}

// NewBankHandler creates a BankHandler backed by the given BankService.
func NewBankHandler(svc *service.BankService) *BankHandler {
	return &BankHandler{service: svc}
}

// Register wires the bank routes into mux.
func (h *BankHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/banks", h.listBanks)
	mux.HandleFunc("GET /api/v1/banks/resolve", h.resolveAccount)
}

// listBanks handles GET /api/v1/banks?country=NG
//
// Returns the full bank list for the requested country, served from a 24-hour
// in-memory cache.
func (h *BankHandler) listBanks(w http.ResponseWriter, r *http.Request) {
	country := r.URL.Query().Get("country")
	if country == "" {
		country = "NG" // default to Nigeria at launch
	}

	banks, err := h.service.ListBanks(r.Context(), country)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(banks))
}

// resolveAccount handles GET /api/v1/banks/resolve?account_number=...&bank_code=...&country=NG
//
// Proxies the NUBAN name-enquiry to Paystack (primary) or Flutterwave
// (fallback).  The provider secret is never exposed to the frontend.
//
// PII: account numbers and names are never written to logs.
func (h *BankHandler) resolveAccount(w http.ResponseWriter, r *http.Request) {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		ip = fwd
	}
	if !resolveRateLimiter.allow(ip) {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(time.Minute.Seconds())))
		response.WriteJSON(w, http.StatusTooManyRequests, response.Response{
			Success: false,
			Error: &response.ErrorBody{
				Code:    "RATE_LIMITED",
				Message: "Too many account resolution requests. Maximum 10 per minute.",
			},
		})
		return
	}

	q := r.URL.Query()
	accountNumber := q.Get("account_number")
	bankCode := q.Get("bank_code")
	country := q.Get("country")
	if country == "" {
		country = "NG"
	}

	// Basic presence check — detailed validation happens in the service.
	if accountNumber == "" || bankCode == "" {
		response.WriteJSON(w, http.StatusBadRequest,
			response.ValidationErr("account_number and bank_code are required"))
		return
	}

	info, err := h.service.ResolveAccount(r.Context(), accountNumber, bankCode, country)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(info))
}

// writeDomainError maps domain errors to the correct HTTP status codes.
func (h *BankHandler) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, bank.ErrAccountNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.Response{
			Success: false,
			Error: &response.ErrorBody{
				Code:    "ACCOUNT_NOT_FOUND",
				Message: "No account found for this number and bank combination.",
			},
		})

	case errors.Is(err, bank.ErrInvalidAccountNumber),
		errors.Is(err, bank.ErrInvalidBankCode),
		errors.Is(err, bank.ErrInvalidCountry):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))

	case errors.Is(err, bank.ErrProviderUnavailable):
		// Both providers are down — return 503 so the frontend can show the
		// "could not verify — you may proceed manually" warning state.
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Response{
			Success: false,
			Error: &response.ErrorBody{
				Code:    "PROVIDER_UNAVAILABLE",
				Message: "Could not verify account — bank verification is temporarily unavailable.",
			},
		})

	default:
		response.WriteJSON(w, http.StatusInternalServerError,
			response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
	}
}
