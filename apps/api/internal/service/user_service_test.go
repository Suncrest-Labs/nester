package service

import (
	"context"
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
	if u.KYCStatus != user.KYCStatusPending {
		t.Errorf("expected pending kyc status")
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
