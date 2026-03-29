package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
)

type UserService struct {
	repo user.UserRepository
}

func NewUserService(repo user.UserRepository) *UserService {
	return &UserService{repo: repo}
}

func (s *UserService) RegisterUser(ctx context.Context, walletAddress, displayName string) (*user.User, error) {
	u := &user.User{
		ID:            uuid.New(),
		WalletAddress: walletAddress,
		DisplayName:   displayName,
		KYCStatus:     user.KYCStatusPending,
	}

	if err := s.repo.Create(ctx, u); err != nil {
		return nil, err
	}

	return u, nil
}

func (s *UserService) GetUser(ctx context.Context, id uuid.UUID) (*user.User, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *UserService) GetUserByWallet(ctx context.Context, address string) (*user.User, error) {
	return s.repo.GetByWalletAddress(ctx, address)
}
