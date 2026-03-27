package middleware

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/api"
)

type rateLimiter struct {
	sync.Mutex
	// map of key (IP or wallet) -> window start time -> request count
	ips     map[string]map[int64]int
	wallets map[string]map[int64]int
}

func newRateLimiter() *rateLimiter {
	rl := &rateLimiter{
		ips:     make(map[string]map[int64]int),
		wallets: make(map[string]map[int64]int),
	}
	// Background cleanup of old windows
	go rl.cleanup()
	return rl
}

func (rl *rateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		rl.Lock()
		now := time.Now().Unix()
		for key, windows := range rl.ips {
			for start := range windows {
				if start < now-60 { // Minute windows
					delete(windows, start)
				}
			}
			if len(windows) == 0 {
				delete(rl.ips, key)
			}
		}
		for key, windows := range rl.wallets {
			for start := range windows {
				if start < now-3600 { // Hour windows
					delete(windows, start)
				}
			}
			if len(windows) == 0 {
				delete(rl.wallets, key)
			}
		}
		rl.Unlock()
	}
}

func (rl *rateLimiter) limitIP(ip string) (bool, time.Duration) {
	rl.Lock()
	defer rl.Unlock()

	now := time.Now().Unix()
	windowStart := now / 60 * 60

	if _, ok := rl.ips[ip]; !ok {
		rl.ips[ip] = make(map[int64]int)
	}

	count := rl.ips[ip][windowStart]
	if count >= 100 {
		return false, time.Duration(60-(now%60)) * time.Second
	}

	rl.ips[ip][windowStart]++
	return true, 0
}

func (rl *rateLimiter) limitWallet(wallet string) (bool, time.Duration) {
	rl.Lock()
	defer rl.Unlock()

	now := time.Now().Unix()
	windowStart := now / 3600 * 3600

	if _, ok := rl.wallets[wallet]; !ok {
		rl.wallets[wallet] = make(map[int64]int)
	}

	count := rl.wallets[wallet][windowStart]
	if count >= 20 {
		return false, time.Duration(3600-(now%3600)) * time.Second
	}

	rl.wallets[wallet][windowStart]++
	return true, 0
}

// RateLimit implements sliding window rate limiting.
func RateLimit(next http.Handler) http.Handler {
	rl := newRateLimiter()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		
		// 1. IP Rate Limiting (100 req/min)
		if ok, retryAfter := rl.limitIP(ip); !ok {
			w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
			api.Error(w, http.StatusTooManyRequests, "global rate limit exceeded: try again later")
			return
		}

		// 2. Wallet Rate Limiting (20 deposits/hour)
		// Extract wallet from X-Wallet-Address header or similar if present.
		// For this implementation, we assume the wallet address is passed in a header.
		wallet := r.Header.Get("X-Wallet-Address")
		if wallet != "" && r.Method == http.MethodPost { 
			// Check if it's a deposit path (basic heuristic)
			if r.URL.Path == "/api/v1/vaults/deposit" || r.URL.Path == "/api/v1/deposits" {
				if ok, retryAfter := rl.limitWallet(wallet); !ok {
					w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
					api.Error(w, http.StatusTooManyRequests, "wallet deposit limit exceeded: try again later")
					return
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}
