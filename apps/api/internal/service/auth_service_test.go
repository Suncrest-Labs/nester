package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type memoryAuthRepository struct {
	nonces   map[string]authNonceRecord
	users    map[string]uuid.UUID
	sessions map[uuid.UUID]AuthSession
}

type authNonceRecord struct {
	walletAddress string
	expiresAt     time.Time
}

func newMemoryAuthRepository() *memoryAuthRepository {
	return &memoryAuthRepository{
		nonces:   make(map[string]authNonceRecord),
		users:    make(map[string]uuid.UUID),
		sessions: make(map[uuid.UUID]AuthSession),
	}
}

func (r *memoryAuthRepository) StoreNonce(_ context.Context, nonce, walletAddress string, expiresAt time.Time) error {
	r.nonces[nonce] = authNonceRecord{walletAddress: walletAddress, expiresAt: expiresAt}
	return nil
}

func (r *memoryAuthRepository) ConsumeNonce(_ context.Context, nonce, walletAddress string, now time.Time) error {
	record, ok := r.nonces[nonce]
	if !ok || record.walletAddress != walletAddress || !record.expiresAt.After(now) {
		return ErrInvalidNonce
	}
	delete(r.nonces, nonce)
	return nil
}

func (r *memoryAuthRepository) CreateOrGetUserByWallet(_ context.Context, walletAddress string) (uuid.UUID, error) {
	if userID, ok := r.users[walletAddress]; ok {
		return userID, nil
	}
	userID := uuid.New()
	r.users[walletAddress] = userID
	return userID, nil
}

func (r *memoryAuthRepository) CreateSession(_ context.Context, session AuthSession) error {
	r.sessions[session.ID] = session
	return nil
}

func (r *memoryAuthRepository) GetSessionByID(_ context.Context, sessionID uuid.UUID) (AuthSession, error) {
	session, ok := r.sessions[sessionID]
	if !ok {
		return AuthSession{}, ErrSessionNotFound
	}
	return session, nil
}

func (r *memoryAuthRepository) DeleteSession(_ context.Context, sessionID uuid.UUID) error {
	delete(r.sessions, sessionID)
	return nil
}

func (r *memoryAuthRepository) ReplaceSession(_ context.Context, previousSessionID uuid.UUID, session AuthSession) error {
	delete(r.sessions, previousSessionID)
	r.sessions[session.ID] = session
	return nil
}

func TestAuthServiceVerifyWalletAndRefresh(t *testing.T) {
	repository := newMemoryAuthRepository()
	service := NewAuthService(repository, AuthConfigInput{
		Issuer:          "nester-test",
		AccessSecret:    "access-secret",
		RefreshSecret:   "refresh-secret",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		NonceTTL:        5 * time.Minute,
	})

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	walletAddress := EncodeStellarAccountID(publicKey)
	nonce, _, err := service.IssueNonce(context.Background(), walletAddress)
	if err != nil {
		t.Fatalf("IssueNonce() error = %v", err)
	}

	message := "Sign in to Nester\nWallet: " + walletAddress + "\nNonce: " + nonce
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(message)))

	tokens, err := service.VerifyWallet(context.Background(), VerifyWalletInput{
		WalletAddress: walletAddress,
		Nonce:         nonce,
		Message:       message,
		Signature:     signature,
	})
	if err != nil {
		t.Fatalf("VerifyWallet() error = %v", err)
	}

	accessPrincipal, err := service.AuthenticateAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatalf("AuthenticateAccessToken() error = %v", err)
	}
	if accessPrincipal.WalletAddress != walletAddress {
		t.Fatalf("expected wallet %q, got %q", walletAddress, accessPrincipal.WalletAddress)
	}

	refreshed, err := service.Refresh(context.Background(), tokens.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if refreshed.RefreshToken == tokens.RefreshToken {
		t.Fatal("expected refresh token rotation")
	}
}

func TestAuthServiceRejectsTamperedSignature(t *testing.T) {
	repository := newMemoryAuthRepository()
	service := NewAuthService(repository, AuthConfigInput{
		Issuer:          "nester-test",
		AccessSecret:    "access-secret",
		RefreshSecret:   "refresh-secret",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		NonceTTL:        5 * time.Minute,
	})

	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	walletAddress := EncodeStellarAccountID(publicKey)
	nonce, _, _ := service.IssueNonce(context.Background(), walletAddress)
	message := "Sign in to Nester\nWallet: " + walletAddress + "\nNonce: " + nonce
	signature := ed25519.Sign(privateKey, []byte(message))
	signature[0] ^= 0xff

	_, err := service.VerifyWallet(context.Background(), VerifyWalletInput{
		WalletAddress: walletAddress,
		Nonce:         nonce,
		Message:       message,
		Signature:     base64.StdEncoding.EncodeToString(signature),
	})
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature, got %v", err)
	}
}

func TestAuthServiceRejectsReplayAttack(t *testing.T) {
	repository := newMemoryAuthRepository()
	service := NewAuthService(repository, AuthConfigInput{
		Issuer:          "nester-test",
		AccessSecret:    "access-secret",
		RefreshSecret:   "refresh-secret",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		NonceTTL:        5 * time.Minute,
	})

	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	walletAddress := EncodeStellarAccountID(publicKey)
	nonce, _, _ := service.IssueNonce(context.Background(), walletAddress)
	message := "Sign in to Nester\nWallet: " + walletAddress + "\nNonce: " + nonce
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(message)))

	if _, err := service.VerifyWallet(context.Background(), VerifyWalletInput{
		WalletAddress: walletAddress,
		Nonce:         nonce,
		Message:       message,
		Signature:     signature,
	}); err != nil {
		t.Fatalf("first VerifyWallet() error = %v", err)
	}

	_, err := service.VerifyWallet(context.Background(), VerifyWalletInput{
		WalletAddress: walletAddress,
		Nonce:         nonce,
		Message:       message,
		Signature:     signature,
	})
	if !errors.Is(err, ErrReplayAttack) {
		t.Fatalf("expected ErrReplayAttack, got %v", err)
	}
}

func TestAuthServiceRejectsExpiredAccessToken(t *testing.T) {
	repository := newMemoryAuthRepository()
	service := NewAuthService(repository, AuthConfigInput{
		Issuer:          "nester-test",
		AccessSecret:    "access-secret",
		RefreshSecret:   "refresh-secret",
		AccessTokenTTL:  time.Minute,
		RefreshTokenTTL: time.Hour,
		NonceTTL:        time.Minute,
	})

	now := time.Date(2026, 3, 27, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	tokens, err := service.issueTokenPair(context.Background(), uuid.New(), "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF")
	if err != nil {
		t.Fatalf("issueTokenPair() error = %v", err)
	}

	service.now = func() time.Time { return now.Add(2 * time.Minute) }
	_, err = service.AuthenticateAccessToken(tokens.AccessToken)
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expected ErrExpiredToken, got %v", err)
	}
}
