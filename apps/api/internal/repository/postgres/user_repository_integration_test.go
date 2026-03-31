package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
)

func TestUserRepositoryIntegrationCRUD(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewUserRepository(db)
	ctx := context.Background()

	u := &user.User{
		ID:            uuid.New(),
		WalletAddress: "G" + uuid.New().String()[:30], // Mock stellar wallet address
		DisplayName:   "Test User",
		KYCStatus:     user.KYCStatusPending,
	}

	// 1. Create
	if err := repository.Create(ctx, u); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// 2. Read by ID
	fetched, err := repository.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if fetched.DisplayName != u.DisplayName {
		t.Errorf("expected display name %q, got %q", u.DisplayName, fetched.DisplayName)
	}
	if fetched.WalletAddress != u.WalletAddress {
		t.Errorf("expected wallet address %q, got %q", u.WalletAddress, fetched.WalletAddress)
	}

	// 3. Read by Wallet Address
	fetchedByWallet, err := repository.GetByWalletAddress(ctx, u.WalletAddress)
	if err != nil {
		t.Fatalf("GetByWalletAddress() error = %v", err)
	}
	if fetchedByWallet.ID != u.ID {
		t.Errorf("expected ID %v, got %v", u.ID, fetchedByWallet.ID)
	}

	// 4. Duplicate wallet test
	duplicateUser := &user.User{
		ID:            uuid.New(),
		WalletAddress: u.WalletAddress, // Same wallet address
		DisplayName:   "Another User",
		KYCStatus:     user.KYCStatusVerified,
	}
	if err := repository.Create(ctx, duplicateUser); err != user.ErrDuplicateWallet {
		t.Fatalf("expected ErrDuplicateWallet on repeat address, got: %v", err)
	}

	// 5. Not found by ID test
	_, err = repository.GetByID(ctx, uuid.New())
	if err != user.ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound for unknown ID, got: %v", err)
	}

	// 6. Not found by Wallet Address test
	_, err = repository.GetByWalletAddress(ctx, "NON_EXISTENT_WALLET")
	if err != user.ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound for unknown address, got: %v", err)
	}
}
