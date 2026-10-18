package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrChallengeNotFound         = errors.New("challenge not found or expired")
	ErrChallengeCapacityExceeded = errors.New("active challenge capacity exceeded")
)

const (
	defaultChallengeCleanupInterval = 30 * time.Second
	DefaultMaxActiveChallenges      = 10000
	challengeCounterKey             = "auth:challenge:__active_count"
)

// ChallengeStore persists single-use auth challenges.
// Implementations must be safe for concurrent use.
type ChallengeStore interface {
	// Set stores challenge for walletAddress, overwriting any existing entry.
	Set(ctx context.Context, walletAddress, challenge string) error
	// GetAndDelete atomically retrieves and removes the challenge.
	// Returns ErrChallengeNotFound when the key is absent or expired.
	GetAndDelete(ctx context.Context, walletAddress string) (string, error)
}

// ── Redis implementation ──────────────────────────────────────────────────────

// setChallengeLua atomically enforces a global cap on concurrently active challenges
// in Redis so rotating-IP challenge floods cannot exhaust Redis memory and evict
// session or rate-limit keys (#1563).
var setChallengeLua = redis.NewScript(`
local key = KEYS[1]
local counterKey = KEYS[2]
local challenge = ARGV[1]
local ttlMs = tonumber(ARGV[2])
local maxCap = tonumber(ARGV[3])

local existed = redis.call("EXISTS", key)
if existed == 0 and maxCap > 0 then
  local current = tonumber(redis.call("GET", counterKey) or "0")
  if current >= maxCap then
    return 0
  end
  local nextVal = redis.call("INCR", counterKey)
  local counterTTL = redis.call("PTTL", counterKey)
  if counterTTL < ttlMs then
    redis.call("PEXPIRE", counterKey, ttlMs)
  end
end

redis.call("SET", key, challenge, "PX", ttlMs)
return 1
`)

var getDelChallengeLua = redis.NewScript(`
local key = KEYS[1]
local counterKey = KEYS[2]
local val = redis.call("GETDEL", key)
if val then
  local current = tonumber(redis.call("GET", counterKey) or "0")
  if current > 0 then
    redis.call("DECR", counterKey)
  end
  return val
end
return false
`)

type RedisChallengeStore struct {
	client              *redis.Client
	ttl                 time.Duration
	maxActiveChallenges int
}

func NewRedisChallengeStore(client *redis.Client, ttl time.Duration) *RedisChallengeStore {
	return &RedisChallengeStore{
		client:              client,
		ttl:                 ttl,
		maxActiveChallenges: DefaultMaxActiveChallenges,
	}
}

// WithMaxActiveChallenges configures the global bound on active Redis challenges.
func (s *RedisChallengeStore) WithMaxActiveChallenges(max int) *RedisChallengeStore {
	s.maxActiveChallenges = max
	return s
}

func (s *RedisChallengeStore) Set(ctx context.Context, walletAddress, challenge string) error {
	ttlMs := s.ttl.Milliseconds()
	if ttlMs <= 0 {
		ttlMs = 1
	}
	res, err := setChallengeLua.Run(
		ctx,
		s.client,
		[]string{challengeKey(walletAddress), challengeCounterKey},
		challenge,
		ttlMs,
		s.maxActiveChallenges,
	).Int()
	if err != nil {
		return fmt.Errorf("redis challenge Set: %w", err)
	}
	if res == 0 {
		return ErrChallengeCapacityExceeded
	}
	return nil
}

func (s *RedisChallengeStore) GetAndDelete(ctx context.Context, walletAddress string) (string, error) {
	val, err := getDelChallengeLua.Run(
		ctx,
		s.client,
		[]string{challengeKey(walletAddress), challengeCounterKey},
	).Text()
	if errors.Is(err, redis.Nil) {
		return "", ErrChallengeNotFound
	}
	if err != nil {
		return "", fmt.Errorf("redis GetDel: %w", err)
	}
	return val, nil
}

func challengeKey(walletAddress string) string {
	return "auth:challenge:" + walletAddress
}

// ── In-memory implementation (dev / single-instance fallback) ─────────────────

type inMemoryEntry struct {
	value     string
	expiresAt time.Time
}

type InMemoryChallengeStore struct {
	mu                  sync.Mutex
	m                   map[string]inMemoryEntry
	ttl                 time.Duration
	maxActiveChallenges int
}

func NewInMemoryChallengeStore(ttl time.Duration) *InMemoryChallengeStore {
	store := &InMemoryChallengeStore{
		m:                   make(map[string]inMemoryEntry),
		ttl:                 ttl,
		maxActiveChallenges: DefaultMaxActiveChallenges,
	}

	go store.cleanupExpiredLoop(challengeCleanupInterval(ttl))

	return store
}

// WithMaxActiveChallenges configures the global bound on active challenges in memory.
func (s *InMemoryChallengeStore) WithMaxActiveChallenges(max int) *InMemoryChallengeStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxActiveChallenges = max
	return s
}

func (s *InMemoryChallengeStore) Set(_ context.Context, walletAddress, challenge string) error {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.m[walletAddress]; !exists && s.maxActiveChallenges > 0 && len(s.m) >= s.maxActiveChallenges {
		//Opportunistically evict expired entries before rejecting.
		for k, entry := range s.m {
			if now.After(entry.expiresAt) {
				delete(s.m, k)
			}
		}
		if len(s.m) >= s.maxActiveChallenges {
			return ErrChallengeCapacityExceeded
		}
	}

	s.m[walletAddress] = inMemoryEntry{value: challenge, expiresAt: now.Add(s.ttl)}
	return nil
}

func (s *InMemoryChallengeStore) GetAndDelete(_ context.Context, walletAddress string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.m[walletAddress]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(s.m, walletAddress)
		return "", ErrChallengeNotFound
	}
	delete(s.m, walletAddress)
	return entry.value, nil
}

func (s *InMemoryChallengeStore) cleanupExpiredLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		s.evictExpired(time.Now())
	}
}

func (s *InMemoryChallengeStore) evictExpired(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for walletAddress, entry := range s.m {
		if now.After(entry.expiresAt) {
			delete(s.m, walletAddress)
		}
	}
}

func challengeCleanupInterval(ttl time.Duration) time.Duration {
	if ttl <= 0 || ttl > defaultChallengeCleanupInterval {
		return defaultChallengeCleanupInterval
	}

	return ttl
}
