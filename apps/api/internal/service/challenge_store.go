package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrChallengeNotFound = errors.New("challenge not found or expired")

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

type RedisChallengeStore struct {
	client *redis.Client
	ttl    time.Duration
}

func NewRedisChallengeStore(client *redis.Client, ttl time.Duration) *RedisChallengeStore {
	return &RedisChallengeStore{client: client, ttl: ttl}
}

func (s *RedisChallengeStore) Set(ctx context.Context, walletAddress, challenge string) error {
	return s.client.Set(ctx, challengeKey(walletAddress), challenge, s.ttl).Err()
}

func (s *RedisChallengeStore) GetAndDelete(ctx context.Context, walletAddress string) (string, error) {
	val, err := s.client.GetDel(ctx, challengeKey(walletAddress)).Result()
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
	mu  sync.Mutex
	m   map[string]inMemoryEntry
	ttl time.Duration
}

func NewInMemoryChallengeStore(ttl time.Duration) *InMemoryChallengeStore {
	return &InMemoryChallengeStore{
		m:   make(map[string]inMemoryEntry),
		ttl: ttl,
	}
}

func (s *InMemoryChallengeStore) Set(_ context.Context, walletAddress, challenge string) error {
	s.mu.Lock()
	s.m[walletAddress] = inMemoryEntry{value: challenge, expiresAt: time.Now().Add(s.ttl)}
	s.mu.Unlock()
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
