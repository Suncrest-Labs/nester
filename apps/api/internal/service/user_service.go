package service

import (
	"context"
	"time"

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
		KYCStatus:     user.KYCStatusUnverified,
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

func (s *UserService) GetUserRoles(ctx context.Context, id uuid.UUID) ([]string, error) {
	return s.repo.GetRoles(ctx, id)
}

func (s *UserService) SubmitKYC(ctx context.Context, userID uuid.UUID, idType, idNumber, frontKey string, backKey *string) error {
	doc := &user.KYCDocument{
		ID:             uuid.New(),
		UserID:         userID,
		IDType:         idType,
		IDNumber:       idNumber,
		FrontObjectKey: frontKey,
		BackObjectKey:  backKey,
	}

	if err := s.repo.SaveKYCDocument(ctx, doc); err != nil {
		return err
	}

	now := time.Now()
	return s.repo.UpdateKYCStatus(ctx, userID, user.KYCStatusPending, nil, &now)
}

func (s *UserService) GetKYCDocument(ctx context.Context, userID uuid.UUID) (*user.KYCDocument, error) {
	return s.repo.GetKYCDocument(ctx, userID)
}

func (s *UserService) UpdateKYCStatus(ctx context.Context, userID uuid.UUID, status user.KYCStatus, reason *string) error {
	now := time.Now()
	return s.repo.UpdateKYCStatus(ctx, userID, status, reason, &now)
}

type UpdateProfileInput struct {
	RiskProfile         *user.RiskProfile `json:"risk_profile"`
	SavingsGoal         *string           `json:"savings_goal"`
	OnboardingCompleted *bool             `json:"onboarding_completed"`
}

func (s *UserService) UpdateProfile(ctx context.Context, userID uuid.UUID, in UpdateProfileInput) (*user.User, error) {
	return s.repo.UpdateProfile(ctx, userID, user.ProfilePatch{
		RiskProfile:         in.RiskProfile,
		SavingsGoal:         in.SavingsGoal,
		OnboardingCompleted: in.OnboardingCompleted,
	})
}

func (s *UserService) GetProfile(ctx context.Context, userID uuid.UUID) (*user.User, error) {
	return s.repo.GetByID(ctx, userID)
}
