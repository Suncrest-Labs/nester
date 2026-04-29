package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryChallengeStore_SetAndGet(t *testing.T) {
	store := NewInMemoryChallengeStore(5 * time.Minute)
	ctx := context.Background()

	require.NoError(t, store.Set(ctx, "WALLET1", "hex123"))

	got, err := store.GetAndDelete(ctx, "WALLET1")
	require.NoError(t, err)
	assert.Equal(t, "hex123", got)
}

func TestInMemoryChallengeStore_GetAndDeleteIsOneTimeUse(t *testing.T) {
	store := NewInMemoryChallengeStore(5 * time.Minute)
	ctx := context.Background()

	require.NoError(t, store.Set(ctx, "WALLET1", "hex123"))
	_, err := store.GetAndDelete(ctx, "WALLET1")
	require.NoError(t, err)

	// Second call must fail.
	_, err = store.GetAndDelete(ctx, "WALLET1")
	assert.ErrorIs(t, err, ErrChallengeNotFound)
}

func TestInMemoryChallengeStore_MissingKeyReturnsNotFound(t *testing.T) {
	store := NewInMemoryChallengeStore(5 * time.Minute)
	_, err := store.GetAndDelete(context.Background(), "NONEXISTENT")
	assert.ErrorIs(t, err, ErrChallengeNotFound)
}

func TestInMemoryChallengeStore_ExpiredEntryReturnsNotFound(t *testing.T) {
	store := NewInMemoryChallengeStore(-1 * time.Millisecond) // already expired
	ctx := context.Background()

	require.NoError(t, store.Set(ctx, "WALLET1", "hex123"))

	_, err := store.GetAndDelete(ctx, "WALLET1")
	assert.ErrorIs(t, err, ErrChallengeNotFound)
}

func TestInMemoryChallengeStore_SetOverwritesPreviousChallenge(t *testing.T) {
	store := NewInMemoryChallengeStore(5 * time.Minute)
	ctx := context.Background()

	require.NoError(t, store.Set(ctx, "WALLET1", "first"))
	require.NoError(t, store.Set(ctx, "WALLET1", "second"))

	got, err := store.GetAndDelete(ctx, "WALLET1")
	require.NoError(t, err)
	assert.Equal(t, "second", got)
}

func TestInMemoryChallengeStore_IsolatesWallets(t *testing.T) {
	store := NewInMemoryChallengeStore(5 * time.Minute)
	ctx := context.Background()

	require.NoError(t, store.Set(ctx, "WALLET_A", "aaaa"))
	require.NoError(t, store.Set(ctx, "WALLET_B", "bbbb"))

	// Deleting WALLET_A must not affect WALLET_B.
	gotA, err := store.GetAndDelete(ctx, "WALLET_A")
	require.NoError(t, err)
	assert.Equal(t, "aaaa", gotA)

	gotB, err := store.GetAndDelete(ctx, "WALLET_B")
	require.NoError(t, err)
	assert.Equal(t, "bbbb", gotB)
}

// TestChallengeStore_ConcurrentAccess verifies that InMemoryChallengeStore is
// safe for concurrent use. 100 goroutines each perform Set, GetAndDelete, and
// a subsequent GetAndDelete (which must return ErrChallengeNotFound) without
// data races. Run with -race to detect any mutex violations.
func TestChallengeStore_ConcurrentAccess(t *testing.T) {
	store := NewInMemoryChallengeStore(5 * time.Minute)
	ctx := context.Background()

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			wallet := fmt.Sprintf("CONCURRENT_WALLET_%d", i)
			challenge := fmt.Sprintf("challenge_%d", i)

			// Set a challenge for this wallet.
			if err := store.Set(ctx, wallet, challenge); err != nil {
				t.Errorf("goroutine %d: Set failed: %v", i, err)
				return
			}

			// Retrieve and delete — must return the challenge we stored.
			got, err := store.GetAndDelete(ctx, wallet)
			if err != nil {
				t.Errorf("goroutine %d: GetAndDelete failed: %v", i, err)
				return
			}
			if got != challenge {
				t.Errorf("goroutine %d: got %q, want %q", i, got, challenge)
			}

			// Second retrieval must return ErrChallengeNotFound (one-time use).
			_, err = store.GetAndDelete(ctx, wallet)
			if err == nil {
				t.Errorf("goroutine %d: expected ErrChallengeNotFound on second GetAndDelete", i)
			}
		}(i)
	}

	wg.Wait()
}
