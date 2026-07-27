package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/crypto"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
)

type UserService struct {
	repo   user.UserRepository
	cipher *crypto.AccountCipher
}

func NewUserService(repo user.UserRepository) *UserService {
	return &UserService{repo: repo}
}

func (s *UserService) WithCipher(c *crypto.AccountCipher) *UserService {
	s.cipher = c
	return s
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

var ErrEncryptionUnavailable = errors.New("encryption is not configured")

func (s *UserService) SubmitKYC(ctx context.Context, userID uuid.UUID, idType, idNumber, frontKey string, backKey *string) error {
	if s.cipher == nil {
		return ErrEncryptionUnavailable
	}

	idNumEnv, err := s.cipher.Encrypt(idNumber)
	if err != nil {
		return err
	}
	frontKeyEnv, err := s.cipher.Encrypt(frontKey)
	if err != nil {
		return err
	}
	fingerprint := s.cipher.Fingerprint(idNumber)

	var backKeyEnc []byte
	var backKeyEnv crypto.CipherEnvelope
	if backKey != nil {
		backKeyEnv, err = s.cipher.Encrypt(*backKey)
		if err != nil {
			return err
		}
		backKeyEnc = backKeyEnv.Ciphertext
	}

	doc := &user.KYCDocument{
		ID:             uuid.New(),
		UserID:         userID,
		IDType:         idType,
		IDNumber:       idNumber,
		FrontObjectKey: frontKey,
		BackObjectKey:  backKey,
	}

	encrypted := &user.EncryptedKYCDoc{
		IDNumberEncrypted:   idNumEnv.Ciphertext,
		IDNumberFingerprint: fingerprint,
		FrontKeyEncrypted:   frontKeyEnv.Ciphertext,
		BackKeyEncrypted:    backKeyEnc,
		KeyVersion:          idNumEnv.KeyVersion,
	}

	if err := s.repo.SaveKYCDocument(ctx, doc, encrypted); err != nil {
		return err
	}

	now := time.Now()
	return s.repo.UpdateKYCStatus(ctx, userID, user.KYCStatusPending, nil, &now)
}

func (s *UserService) GetKYCDocument(ctx context.Context, userID uuid.UUID) (*user.KYCDocument, error) {
	if s.cipher == nil {
		return nil, ErrEncryptionUnavailable
	}

	doc, encrypted, err := s.repo.GetKYCDocument(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Decrypt id_number — fall back to plaintext column if ciphertext is nil
	// (legacy rows created before the encryption migration was backfilled).
	if encrypted.IDNumberEncrypted != nil {
		plain, err := s.cipher.Decrypt(crypto.CipherEnvelope{
			KeyVersion: encrypted.KeyVersion,
			Ciphertext: encrypted.IDNumberEncrypted,
		})
		if err != nil {
			return nil, err
		}
		doc.IDNumber = plain
	}

	// Decrypt front_object_key
	if encrypted.FrontKeyEncrypted != nil {
		plain, err := s.cipher.Decrypt(crypto.CipherEnvelope{
			KeyVersion: encrypted.KeyVersion,
			Ciphertext: encrypted.FrontKeyEncrypted,
		})
		if err != nil {
			return nil, err
		}
		doc.FrontObjectKey = plain
	}

	// Decrypt back_object_key
	if encrypted.BackKeyEncrypted != nil {
		plain, err := s.cipher.Decrypt(crypto.CipherEnvelope{
			KeyVersion: encrypted.KeyVersion,
			Ciphertext: encrypted.BackKeyEncrypted,
		})
		if err != nil {
			return nil, err
		}
		doc.BackObjectKey = &plain
	}

	return doc, nil
}

func (s *UserService) UpdateKYCStatus(ctx context.Context, userID uuid.UUID, status user.KYCStatus, reason *string) error {
	now := time.Now()
	return s.repo.UpdateKYCStatus(ctx, userID, status, reason, &now)
}

type UpdateProfileInput struct {
	RiskProfile         *user.RiskProfile `json:"risk_profile"`
	SavingsGoal         *string           `json:"savings_goal"`
	OnboardingCompleted *bool             `json:"onboarding_completed"`
	Timezone            *string           `json:"timezone"`
}

func (s *UserService) UpdateProfile(ctx context.Context, userID uuid.UUID, in UpdateProfileInput) (*user.User, error) {
	return s.repo.UpdateProfile(ctx, userID, user.ProfilePatch{
		RiskProfile:         in.RiskProfile,
		SavingsGoal:         in.SavingsGoal,
		OnboardingCompleted: in.OnboardingCompleted,
		Timezone:            in.Timezone,
	})
}

func (s *UserService) GetProfile(ctx context.Context, userID uuid.UUID) (*user.User, error) {
	return s.repo.GetByID(ctx, userID)
}
