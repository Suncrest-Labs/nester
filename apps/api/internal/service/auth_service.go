package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/stellar/go/keypair"
	"github.com/suncrestlabs/nester/apps/api/internal/auth"
)

var (
	ErrChallengeExpired = errors.New("challenge expired or invalid")
	ErrSignatureInvalid = errors.New("signature is invalid")
	ErrWalletInvalid    = errors.New("wallet address is invalid")
)

type AuthService interface {
	GenerateChallenge(ctx context.Context, walletAddress string) (string, error)
	VerifyAndIssue(ctx context.Context, walletAddress, signature, challenge string) (string, error)
}

type AuthConfig interface {
	Secret() string
	TokenExpiry() time.Duration
	ChallengeExpiry() time.Duration
}

type authService struct {
	store       ChallengeStore
	userService *UserService
	config      AuthConfig
}

func NewAuthService(store ChallengeStore, userService *UserService, cfg AuthConfig) AuthService {
	return &authService{
		store:       store,
		userService: userService,
		config:      cfg,
	}
}

func (s *authService) GenerateChallenge(ctx context.Context, walletAddress string) (string, error) {
	if _, err := keypair.ParseAddress(walletAddress); err != nil {
		return "", ErrWalletInvalid
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	challenge := hex.EncodeToString(b)

	if err := s.store.Set(ctx, walletAddress, challenge); err != nil {
		return "", err
	}
	return challenge, nil
}

func (s *authService) VerifyAndIssue(ctx context.Context, walletAddress, signature string, challenge string) (string, error) {
	stored, err := s.store.GetAndDelete(ctx, walletAddress)
	if err != nil {
		if errors.Is(err, ErrChallengeNotFound) {
			return "", ErrChallengeExpired
		}
		return "", err
	}

	if stored != challenge {
		return "", ErrChallengeExpired
	}

	kp, err := keypair.ParseAddress(walletAddress)
	if err != nil {
		return "", ErrWalletInvalid
	}

	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return "", ErrSignatureInvalid
	}

	if err := kp.Verify([]byte(challenge), sigBytes); err != nil {
		return "", ErrSignatureInvalid
	}

	user, err := s.userService.GetUserByWallet(ctx, walletAddress)
	if err != nil {
		user, err = s.userService.RegisterUser(ctx, walletAddress, walletAddress[:8])
		if err != nil {
			return "", err
		}
	}

	roles, err := s.userService.GetUserRoles(ctx, user.ID)
	if err != nil {
		return "", err
	}

	claims := auth.Claims{
		Subject:       user.ID.String(),
		WalletAddress: walletAddress,
		IssuedAt:      time.Now().Unix(),
		ExpiresAt:     time.Now().Add(s.config.TokenExpiry()).Unix(),
		Roles:         roles,
	}

	return auth.MakeJWT(claims, s.config.Secret())
}
