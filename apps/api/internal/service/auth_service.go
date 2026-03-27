package service

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidWalletAddress = errors.New("invalid wallet address")
	ErrInvalidNonce         = errors.New("invalid or expired nonce")
	ErrInvalidSignature     = errors.New("invalid wallet signature")
	ErrReplayAttack         = errors.New("nonce already used or expired")
	ErrInvalidToken         = errors.New("invalid token")
	ErrExpiredToken         = errors.New("token expired")
	ErrSessionNotFound      = errors.New("session not found")
)

type AuthRepository interface {
	StoreNonce(ctx context.Context, nonce, walletAddress string, expiresAt time.Time) error
	ConsumeNonce(ctx context.Context, nonce, walletAddress string, now time.Time) error
	CreateOrGetUserByWallet(ctx context.Context, walletAddress string) (uuid.UUID, error)
	CreateSession(ctx context.Context, session AuthSession) error
	GetSessionByID(ctx context.Context, sessionID uuid.UUID) (AuthSession, error)
	DeleteSession(ctx context.Context, sessionID uuid.UUID) error
	ReplaceSession(ctx context.Context, previousSessionID uuid.UUID, session AuthSession) error
}

type AuthService struct {
	repository      AuthRepository
	issuer          string
	accessSecret    []byte
	refreshSecret   []byte
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
	nonceTTL        time.Duration
	now             func() time.Time
}

type AuthSession struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	WalletAddress    string
	RefreshTokenHash string
	ExpiresAt        time.Time
	CreatedAt        time.Time
}

type AuthPrincipal struct {
	UserID        uuid.UUID
	WalletAddress string
	SessionID     uuid.UUID
	TokenType     string
	ExpiresAt     time.Time
}

type TokenPair struct {
	AccessToken           string    `json:"access_token"`
	RefreshToken          string    `json:"refresh_token"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
	UserID                uuid.UUID `json:"user_id"`
	WalletAddress         string    `json:"wallet_address"`
}

type VerifyWalletInput struct {
	WalletAddress string
	Nonce         string
	Message       string
	Signature     string
}

type AuthConfigInput struct {
	Issuer          string
	AccessSecret    string
	RefreshSecret   string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	NonceTTL        time.Duration
}

func NewAuthService(repository AuthRepository, cfg AuthConfigInput) *AuthService {
	return &AuthService{
		repository:      repository,
		issuer:          strings.TrimSpace(cfg.Issuer),
		accessSecret:    []byte(cfg.AccessSecret),
		refreshSecret:   []byte(cfg.RefreshSecret),
		accessTokenTTL:  cfg.AccessTokenTTL,
		refreshTokenTTL: cfg.RefreshTokenTTL,
		nonceTTL:        cfg.NonceTTL,
		now:             func() time.Time { return time.Now().UTC() },
	}
}

func (s *AuthService) IssueNonce(ctx context.Context, walletAddress string) (string, time.Time, error) {
	normalizedWallet, err := normalizeWalletAddress(walletAddress)
	if err != nil {
		return "", time.Time{}, err
	}

	nonce, err := randomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}

	expiresAt := s.now().Add(s.nonceTTL)
	if err := s.repository.StoreNonce(ctx, nonce, normalizedWallet, expiresAt); err != nil {
		return "", time.Time{}, err
	}

	return nonce, expiresAt, nil
}

func (s *AuthService) VerifyWallet(ctx context.Context, input VerifyWalletInput) (TokenPair, error) {
	normalizedWallet, err := normalizeWalletAddress(input.WalletAddress)
	if err != nil {
		return TokenPair{}, err
	}

	nonce := strings.TrimSpace(input.Nonce)
	message := strings.TrimSpace(input.Message)
	if nonce == "" || message == "" {
		return TokenPair{}, ErrInvalidNonce
	}

	if !strings.Contains(message, nonce) || !strings.Contains(message, normalizedWallet) {
		return TokenPair{}, ErrInvalidSignature
	}

	if err := verifyWalletSignature(normalizedWallet, message, input.Signature); err != nil {
		return TokenPair{}, err
	}

	if err := s.repository.ConsumeNonce(ctx, nonce, normalizedWallet, s.now()); err != nil {
		if errors.Is(err, ErrInvalidNonce) {
			return TokenPair{}, ErrReplayAttack
		}
		return TokenPair{}, err
	}

	userID, err := s.repository.CreateOrGetUserByWallet(ctx, normalizedWallet)
	if err != nil {
		return TokenPair{}, err
	}

	return s.issueTokenPair(ctx, userID, normalizedWallet)
}

func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	principal, err := s.parseToken(refreshToken, s.refreshSecret, "refresh")
	if err != nil {
		return TokenPair{}, err
	}

	session, err := s.repository.GetSessionByID(ctx, principal.SessionID)
	if err != nil {
		return TokenPair{}, err
	}

	if session.ExpiresAt.Before(s.now()) {
		_ = s.repository.DeleteSession(ctx, session.ID)
		return TokenPair{}, ErrExpiredToken
	}

	if session.RefreshTokenHash != hashToken(refreshToken) {
		return TokenPair{}, ErrInvalidToken
	}

	return s.rotateSession(ctx, session.UserID, session.WalletAddress, session.ID)
}

func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	principal, err := s.parseToken(refreshToken, s.refreshSecret, "refresh")
	if err != nil {
		return err
	}

	session, err := s.repository.GetSessionByID(ctx, principal.SessionID)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil
		}
		return err
	}

	if session.RefreshTokenHash != hashToken(refreshToken) {
		return ErrInvalidToken
	}

	return s.repository.DeleteSession(ctx, session.ID)
}

func (s *AuthService) AuthenticateAccessToken(token string) (AuthPrincipal, error) {
	return s.parseToken(token, s.accessSecret, "access")
}

func (s *AuthService) issueTokenPair(ctx context.Context, userID uuid.UUID, walletAddress string) (TokenPair, error) {
	sessionID := uuid.New()
	return s.createTokenPair(ctx, userID, walletAddress, sessionID, uuid.Nil)
}

func (s *AuthService) rotateSession(ctx context.Context, userID uuid.UUID, walletAddress string, previousSessionID uuid.UUID) (TokenPair, error) {
	return s.createTokenPair(ctx, userID, walletAddress, uuid.New(), previousSessionID)
}

func (s *AuthService) createTokenPair(
	ctx context.Context,
	userID uuid.UUID,
	walletAddress string,
	sessionID uuid.UUID,
	previousSessionID uuid.UUID,
) (TokenPair, error) {
	now := s.now()
	accessExpiresAt := now.Add(s.accessTokenTTL)
	refreshExpiresAt := now.Add(s.refreshTokenTTL)

	accessToken, err := s.signToken(tokenClaims{
		Subject:   walletAddress,
		Issuer:    s.issuer,
		Type:      "access",
		UserID:    userID.String(),
		SessionID: sessionID.String(),
		IssuedAt:  now.Unix(),
		ExpiresAt: accessExpiresAt.Unix(),
	})
	if err != nil {
		return TokenPair{}, err
	}

	refreshToken, err := s.signRefreshToken(tokenClaims{
		Subject:   walletAddress,
		Issuer:    s.issuer,
		Type:      "refresh",
		UserID:    userID.String(),
		SessionID: sessionID.String(),
		IssuedAt:  now.Unix(),
		ExpiresAt: refreshExpiresAt.Unix(),
	})
	if err != nil {
		return TokenPair{}, err
	}

	session := AuthSession{
		ID:               sessionID,
		UserID:           userID,
		WalletAddress:    walletAddress,
		RefreshTokenHash: hashToken(refreshToken),
		ExpiresAt:        refreshExpiresAt,
		CreatedAt:        now,
	}

	if previousSessionID == uuid.Nil {
		if err := s.repository.CreateSession(ctx, session); err != nil {
			return TokenPair{}, err
		}
	} else {
		if err := s.repository.ReplaceSession(ctx, previousSessionID, session); err != nil {
			return TokenPair{}, err
		}
	}

	return TokenPair{
		AccessToken:           accessToken,
		RefreshToken:          refreshToken,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshTokenExpiresAt: refreshExpiresAt,
		UserID:                userID,
		WalletAddress:         walletAddress,
	}, nil
}

type tokenClaims struct {
	Subject   string `json:"sub"`
	Issuer    string `json:"iss"`
	Type      string `json:"typ"`
	UserID    string `json:"uid"`
	SessionID string `json:"sid"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

func (s *AuthService) signToken(claims tokenClaims) (string, error) {
	return signJWT(claims, s.accessSecret)
}

func (s *AuthService) signRefreshToken(claims tokenClaims) (string, error) {
	return signJWT(claims, s.refreshSecret)
}

func (s *AuthService) parseToken(raw string, secret []byte, expectedType string) (AuthPrincipal, error) {
	claims, err := parseAndVerifyJWT(raw, secret)
	if err != nil {
		return AuthPrincipal{}, err
	}

	if claims.Issuer != s.issuer || claims.Type != expectedType {
		return AuthPrincipal{}, ErrInvalidToken
	}

	if claims.ExpiresAt <= s.now().Unix() {
		return AuthPrincipal{}, ErrExpiredToken
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		return AuthPrincipal{}, ErrInvalidToken
	}

	sessionID, err := uuid.Parse(claims.SessionID)
	if err != nil {
		return AuthPrincipal{}, ErrInvalidToken
	}

	return AuthPrincipal{
		UserID:        userID,
		WalletAddress: claims.Subject,
		SessionID:     sessionID,
		TokenType:     claims.Type,
		ExpiresAt:     time.Unix(claims.ExpiresAt, 0).UTC(),
	}, nil
}

func signJWT(claims tokenClaims, secret []byte) (string, error) {
	header := map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	}

	headerBytes, err := json.Marshal(header)
	if err != nil {
		return "", err
	}

	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	headerPart := base64.RawURLEncoding.EncodeToString(headerBytes)
	payloadPart := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := headerPart + "." + payloadPart

	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + signature, nil
}

func parseAndVerifyJWT(raw string, secret []byte) (tokenClaims, error) {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) != 3 {
		return tokenClaims{}, ErrInvalidToken
	}

	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(signingInput))
	expectedSignature := mac.Sum(nil)

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return tokenClaims{}, ErrInvalidToken
	}

	if !hmac.Equal(signature, expectedSignature) {
		return tokenClaims{}, ErrInvalidToken
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return tokenClaims{}, ErrInvalidToken
	}

	var claims tokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return tokenClaims{}, ErrInvalidToken
	}

	return claims, nil
}

func verifyWalletSignature(walletAddress, message, rawSignature string) error {
	publicKey, err := decodeStellarAccountID(walletAddress)
	if err != nil {
		return ErrInvalidWalletAddress
	}

	signature, err := decodeSignature(rawSignature)
	if err != nil {
		return ErrInvalidSignature
	}

	if !ed25519.Verify(publicKey, []byte(message), signature) {
		return ErrInvalidSignature
	}

	return nil
}

func decodeSignature(raw string) ([]byte, error) {
	candidates := []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		base64.URLEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
		hex.DecodeString,
	}

	trimmed := strings.TrimSpace(raw)
	for _, candidate := range candidates {
		decoded, err := candidate(trimmed)
		if err == nil && len(decoded) > 0 {
			return decoded, nil
		}
	}

	return nil, ErrInvalidSignature
}

func normalizeWalletAddress(walletAddress string) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(walletAddress))
	if _, err := decodeStellarAccountID(normalized); err != nil {
		return "", ErrInvalidWalletAddress
	}
	return normalized, nil
}

func decodeStellarAccountID(accountID string) (ed25519.PublicKey, error) {
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(accountID)
	if err != nil {
		return nil, err
	}

	if len(decoded) != 35 {
		return nil, fmt.Errorf("invalid decoded length")
	}

	versionByte := decoded[0]
	if versionByte != 6<<3 {
		return nil, fmt.Errorf("invalid version byte")
	}

	payload := decoded[:33]
	checksum := decoded[33:]
	expected := crc16XModem(payload)
	if checksum[0] != byte(expected&0xff) || checksum[1] != byte(expected>>8) {
		return nil, fmt.Errorf("invalid checksum")
	}

	return ed25519.PublicKey(payload[1:]), nil
}

func crc16XModem(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func randomToken(byteLength int) (string, error) {
	buffer := make([]byte, byteLength)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func EncodeStellarAccountID(publicKey ed25519.PublicKey) string {
	payload := make([]byte, 33)
	payload[0] = 6 << 3
	copy(payload[1:], publicKey)

	checksum := crc16XModem(payload)
	raw := make([]byte, 35)
	copy(raw, payload)
	binary.LittleEndian.PutUint16(raw[33:], checksum)

	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
}
