package user

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

type KYCStatus string

const (
	KYCStatusUnverified KYCStatus = "unverified"
	KYCStatusPending    KYCStatus = "pending"
	KYCStatusVerified   KYCStatus = "verified"
	KYCStatusRejected   KYCStatus = "rejected"
)

type RiskProfile string

const (
	RiskProfileConservative RiskProfile = "conservative"
	RiskProfileModerate     RiskProfile = "moderate"
	RiskProfileAggressive   RiskProfile = "aggressive"
)

type User struct {
	ID                 uuid.UUID  `json:"id"`
	WalletAddress      string     `json:"wallet_address"`
	DisplayName        string     `json:"display_name"`
	KYCStatus          KYCStatus  `json:"kyc_status"`
	Tier               string     `json:"tier"`
	KYCSubmittedAt     *time.Time `json:"kyc_submitted_at,omitempty"`
	KYCReviewedAt      *time.Time `json:"kyc_reviewed_at,omitempty"`
	KYCRejectionReason *string    `json:"kyc_rejection_reason,omitempty"`
  RiskProfile         *RiskProfile `json:"risk_profile,omitempty"`
	SavingsGoal         *string     `json:"savings_goal,omitempty"`
	OnboardingCompleted bool        `json:"onboarding_completed"`
	LastLoginAt        *time.Time `json:"last_login_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type KYCDocument struct {
	ID             uuid.UUID `json:"id"`
	UserID         uuid.UUID `json:"user_id"`
	IDType         string    `json:"id_type"`
	IDNumber       string    `json:"id_number"`
	FrontObjectKey string    `json:"front_object_key"`
	BackObjectKey  *string   `json:"back_object_key,omitempty"`
	SubmittedAt    time.Time `json:"submitted_at"`
}

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrDuplicateWallet   = errors.New("wallet address already registered")
	ErrInvalidWallet     = errors.New("invalid wallet address")
)

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	GetByWalletAddress(ctx context.Context, addr string) (*User, error)
	GetRoles(ctx context.Context, id uuid.UUID) ([]string, error)
	SaveKYCDocument(ctx context.Context, doc *KYCDocument) error
	GetKYCDocument(ctx context.Context, userID uuid.UUID) (*KYCDocument, error)
	UpdateKYCStatus(ctx context.Context, userID uuid.UUID, status KYCStatus, reason *string, reviewedAt *time.Time) error
	UpdateProfile(ctx context.Context, id uuid.UUID, patch ProfilePatch) (*User, error)
}

// ProfilePatch holds optional user profile fields for PATCH /api/v1/users/profile.
type ProfilePatch struct {
	RiskProfile         *RiskProfile
	SavingsGoal         *string
	OnboardingCompleted *bool
}
