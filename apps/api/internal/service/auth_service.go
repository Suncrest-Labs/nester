package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/stellar/go/keypair"
	"github.com/suncrestlabs/nester/apps/api/internal/auth"
)

var (
	ErrChallengeExpired = errors.New("challenge expired or invalid")
	ErrSignatureInvalid = errors.New("signature is invalid")
	ErrWalletInvalid    = errors.New("wallet address is invalid")
)

type challengeData struct {
	Challenge string
	ExpiresAt time.Time
}

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
	mu          sync.RWMutex
	challenges  map[string]challengeData
	userService *UserService
	config      AuthConfig
}

func NewAuthService(userService *UserService, cfg AuthConfig) AuthService {
	return &authService{
		challenges:  make(map[string]challengeData),
		userService: userService,
		config:      cfg,
	}
}

func (s *authService) GenerateChallenge(ctx context.Context, walletAddress string) (string, error) {
	// Validate wallet format first
	if _, err := keypair.ParseAddress(walletAddress); err != nil {
		return "", ErrWalletInvalid
	}

	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	challenge := hex.EncodeToString(bytes)

	s.mu.Lock()
	s.challenges[walletAddress] = challengeData{
		Challenge: challenge,
		ExpiresAt: time.Now().Add(s.config.ChallengeExpiry()),
	}
	s.mu.Unlock()

	return challenge, nil
}

func (s *authService) VerifyAndIssue(ctx context.Context, walletAddress, signature string, challenge string) (string, error) {
	s.mu.Lock()
	data, ok := s.challenges[walletAddress]
	if ok {
		delete(s.challenges, walletAddress) // One-time use challenge
	}
	s.mu.Unlock()

	if !ok || time.Now().After(data.ExpiresAt) || data.Challenge != challenge {
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

	// Try to get user, if not found, register them
	user, err := s.userService.GetUserByWallet(ctx, walletAddress)
	if err != nil {
		// Register a new user if one doesn't exist
		// displayName defaults to first 8 chars of wallet
		user, err = s.userService.RegisterUser(ctx, walletAddress, walletAddress[:8])
		if err != nil {
			return "", err
		}
	}

	claims := auth.Claims{
		Subject:       user.ID.String(),
		WalletAddress: walletAddress,
		IssuedAt:      time.Now().Unix(),
		ExpiresAt:     time.Now().Add(s.config.TokenExpiry()).Unix(),
	}

	token, err := auth.MakeJWT(claims, s.config.Secret())
	if err != nil {
		return "", err
	}

	return token, nil
}
