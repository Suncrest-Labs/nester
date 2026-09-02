package user

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

type RiskProfile string

const (
	RiskProfileConservative RiskProfile = "conservative"
	RiskProfileModerate     RiskProfile = "moderate"
	RiskProfileAggressive   RiskProfile = "aggressive"
)

type User struct {
	ID                  uuid.UUID    `json:"id"`
	WalletAddress       string       `json:"wallet_address"`
	DisplayName         string       `json:"display_name"`
	Tier                string       `json:"tier"`
	RiskProfile         *RiskProfile `json:"risk_profile,omitempty"`
	SavingsGoal         *string      `json:"savings_goal,omitempty"`
	OnboardingCompleted bool         `json:"onboarding_completed"`
	LastLoginAt         *time.Time   `json:"last_login_at,omitempty"`
	Timezone            string       `json:"timezone"`
	CreatedAt           time.Time    `json:"created_at"`
	UpdatedAt           time.Time    `json:"updated_at"`
}

var (
	ErrUserNotFound    = errors.New("user not found")
	ErrDuplicateWallet = errors.New("wallet address already registered")
	ErrInvalidWallet   = errors.New("invalid wallet address")
)

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	GetByWalletAddress(ctx context.Context, addr string) (*User, error)
	GetRoles(ctx context.Context, id uuid.UUID) ([]string, error)
	UpdateProfile(ctx context.Context, id uuid.UUID, patch ProfilePatch) (*User, error)
}

// ProfilePatch holds optional user profile fields for PATCH /api/v1/users/profile.
type ProfilePatch struct {
	RiskProfile         *RiskProfile
	SavingsGoal         *string
	OnboardingCompleted *bool
	Timezone            *string
}
