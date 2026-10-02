package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
)

type mockUserRepository struct {
	users map[uuid.UUID]*user.User
}

func newMockUserRepository() *mockUserRepository {
	return &mockUserRepository{
		users: make(map[uuid.UUID]*user.User),
	}
}

func (m *mockUserRepository) Create(ctx context.Context, u *user.User) error {
	for _, existing := range m.users {
		if existing.WalletAddress == u.WalletAddress {
			return user.ErrDuplicateWallet
		}
	}
	u.CreatedAt = time.Now()
	u.UpdatedAt = time.Now()
	m.users[u.ID] = u
	return nil
}

func (m *mockUserRepository) GetByID(ctx context.Context, id uuid.UUID) (*user.User, error) {
	if u, exists := m.users[id]; exists {
		return u, nil
	}
	return nil, user.ErrUserNotFound
}

func (m *mockUserRepository) GetByWalletAddress(ctx context.Context, addr string) (*user.User, error) {
	for _, u := range m.users {
		if u.WalletAddress == addr {
			return u, nil
		}
	}
	return nil, user.ErrUserNotFound
}

func (m *mockUserRepository) GetRoles(_ context.Context, _ uuid.UUID) ([]string, error) {
	return []string{}, nil
}

func (m *mockUserRepository) UpdateProfile(_ context.Context, id uuid.UUID, patch user.ProfilePatch) (*user.User, error) {
	u, err := m.GetByID(context.Background(), id)
	if err != nil {
		return nil, err
	}
	if patch.RiskProfile != nil {
		u.RiskProfile = patch.RiskProfile
	}
	if patch.SavingsGoal != nil {
		u.SavingsGoal = patch.SavingsGoal
	}
	if patch.OnboardingCompleted != nil {
		u.OnboardingCompleted = *patch.OnboardingCompleted
	}
	if patch.Timezone != nil {
		u.Timezone = *patch.Timezone
	}
	m.users[id] = u
	return u, nil
}

func TestUserService_RegisterUser(t *testing.T) {
	ctx := context.Background()
	repo := newMockUserRepository()
	svc := NewUserService(repo)

	// Test successful registration
	u, err := svc.RegisterUser(ctx, "G-ADDRESS-TEST", "John Doe")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if u.ID == uuid.Nil {
		t.Errorf("expected generated UUID")
	}

	// Test duplicate wallet
	_, err = svc.RegisterUser(ctx, "G-ADDRESS-TEST", "Jane Doe")
	if err != user.ErrDuplicateWallet {
		t.Errorf("expected ErrDuplicateWallet, got %v", err)
	}
}

func TestUserService_GetUser(t *testing.T) {
	ctx := context.Background()
	repo := newMockUserRepository()
	svc := NewUserService(repo)

	// Seed user
	u, _ := svc.RegisterUser(ctx, "G-SOME-WALLET", "Test User")

	// 1. Valid fetch
	fetched, err := svc.GetUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if fetched.WalletAddress != "G-SOME-WALLET" {
		t.Errorf("expected G-SOME-WALLET")
	}

	// 2. Fetch unknown
	_, err = svc.GetUser(ctx, uuid.New())
	if err != user.ErrUserNotFound {
		t.Errorf("expected user not found error")
	}
}

func TestUserService_GetUserByWallet(t *testing.T) {
	ctx := context.Background()
	repo := newMockUserRepository()
	svc := NewUserService(repo)

	// Seed user
	u, _ := svc.RegisterUser(ctx, "G-WALLET-ABC", "Test User")

	// 1. Valid fetch
	fetched, err := svc.GetUserByWallet(ctx, "G-WALLET-ABC")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if fetched.ID != u.ID {
		t.Errorf("expected ID match")
	}

	// 2. Fetch unknown
	_, err = svc.GetUserByWallet(ctx, "G-UNKNOWN")
	if err != user.ErrUserNotFound {
		t.Errorf("expected user not found error")
	}
}

// TestUserService_RegisterUser_DuplicateWalletIsDomainError guards against a
// regression where a duplicate wallet surfaces as a raw repository/DB error
// instead of the typed user.ErrDuplicateWallet sentinel the handler layer
// switches on (writeDomainError maps it to 409, everything else falls
// through to a generic 500).
func TestUserService_RegisterUser_DuplicateWalletIsDomainError(t *testing.T) {
	ctx := context.Background()
	repo := newMockUserRepository()
	svc := NewUserService(repo)

	if _, err := svc.RegisterUser(ctx, "G-DOMAIN-ERR", "First"); err != nil {
		t.Fatalf("seed registration failed: %v", err)
	}

	_, err := svc.RegisterUser(ctx, "G-DOMAIN-ERR", "Second")
	if !errors.Is(err, user.ErrDuplicateWallet) {
		t.Fatalf("expected errors.Is(err, ErrDuplicateWallet), got %v", err)
	}
}

func TestUserService_UpdateProfile(t *testing.T) {
	ctx := context.Background()
	repo := newMockUserRepository()
	svc := NewUserService(repo)

	u, err := svc.RegisterUser(ctx, "G-PROFILE-USER", "Profile Owner")
	if err != nil {
		t.Fatalf("seed registration failed: %v", err)
	}

	// Baseline: DisplayName and WalletAddress are untouched by any of the
	// partial updates below.
	wantWallet := u.WalletAddress
	wantDisplayName := u.DisplayName

	t.Run("updates only the fields provided", func(t *testing.T) {
		goal := "buy a house"
		updated, err := svc.UpdateProfile(ctx, u.ID, UpdateProfileInput{
			SavingsGoal: &goal,
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.SavingsGoal == nil || *updated.SavingsGoal != goal {
			t.Errorf("expected SavingsGoal %q, got %v", goal, updated.SavingsGoal)
		}
		if updated.RiskProfile != nil {
			t.Errorf("expected RiskProfile to remain unset, got %v", *updated.RiskProfile)
		}
		if updated.OnboardingCompleted {
			t.Errorf("expected OnboardingCompleted to remain false")
		}
		if updated.WalletAddress != wantWallet || updated.DisplayName != wantDisplayName {
			t.Errorf("partial update must not touch unrelated fields")
		}
	})

	t.Run("a second partial update does not clobber the first field", func(t *testing.T) {
		onboarded := true
		updated, err := svc.UpdateProfile(ctx, u.ID, UpdateProfileInput{
			OnboardingCompleted: &onboarded,
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !updated.OnboardingCompleted {
			t.Errorf("expected OnboardingCompleted true")
		}
		if updated.SavingsGoal == nil || *updated.SavingsGoal != "buy a house" {
			t.Errorf("expected the earlier SavingsGoal update to survive, got %v", updated.SavingsGoal)
		}
	})

	t.Run("risk profile and timezone update independently", func(t *testing.T) {
		rp := user.RiskProfileAggressive
		tz := "America/New_York"
		updated, err := svc.UpdateProfile(ctx, u.ID, UpdateProfileInput{
			RiskProfile: &rp,
			Timezone:    &tz,
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if updated.RiskProfile == nil || *updated.RiskProfile != user.RiskProfileAggressive {
			t.Errorf("expected RiskProfile aggressive, got %v", updated.RiskProfile)
		}
		if updated.Timezone != tz {
			t.Errorf("expected Timezone %q, got %q", tz, updated.Timezone)
		}
	})

	t.Run("unknown user returns ErrUserNotFound", func(t *testing.T) {
		goal := "irrelevant"
		_, err := svc.UpdateProfile(ctx, uuid.New(), UpdateProfileInput{SavingsGoal: &goal})
		if !errors.Is(err, user.ErrUserNotFound) {
			t.Fatalf("expected ErrUserNotFound, got %v", err)
		}
	})
}
