package service

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stellar/go/keypair"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
)

type mockAuthConfig struct {
	secret          string
	tokenExpiry     time.Duration
	challengeExpiry time.Duration
}

func (m mockAuthConfig) Secret() string            { return m.secret }
func (m mockAuthConfig) TokenExpiry() time.Duration { return m.tokenExpiry }
func (m mockAuthConfig) ChallengeExpiry() time.Duration { return m.challengeExpiry }

type mockAuthUserRepository struct {
	users map[string]*user.User
}

func (m *mockAuthUserRepository) Create(ctx context.Context, u *user.User) error {
	m.users[u.WalletAddress] = u
	return nil
}

func (m *mockAuthUserRepository) GetByID(ctx context.Context, id uuid.UUID) (*user.User, error) {
	return nil, errors.New("not found")
}

func (m *mockAuthUserRepository) GetByWalletAddress(ctx context.Context, address string) (*user.User, error) {
	if u, ok := m.users[address]; ok {
		return u, nil
	}
	return nil, errors.New("not found")
}

func setupAuthService() (AuthService, *keypair.Full) {
	cfg := mockAuthConfig{
		secret:          "test-super-secret-key-that-is-32-bytes-long",
		tokenExpiry:     1 * time.Hour,
		challengeExpiry: 5 * time.Minute,
	}

	repo := &mockAuthUserRepository{users: make(map[string]*user.User)}
	userService := NewUserService(repo)
	authSvc := NewAuthService(userService, cfg)

	kp, _ := keypair.Random()
	return authSvc, kp
}

func TestAuthService_GenerateChallenge(t *testing.T) {
	svc, kp := setupAuthService()

	challenge, err := svc.GenerateChallenge(context.Background(), kp.Address())
	require.NoError(t, err)
	assert.NotEmpty(t, challenge)
	assert.Len(t, challenge, 64) // hex encoding of 32 bytes

	// Invalid wallet
	_, err = svc.GenerateChallenge(context.Background(), "invalid-wallet")
	assert.ErrorIs(t, err, ErrWalletInvalid)
}

func TestAuthService_VerifyAndIssue_Success(t *testing.T) {
	svc, kp := setupAuthService()

	challenge, err := svc.GenerateChallenge(context.Background(), kp.Address())
	require.NoError(t, err)

	sigBytes, err := kp.Sign([]byte(challenge))
	require.NoError(t, err)
	sigStr := base64.StdEncoding.EncodeToString(sigBytes)

	token, err := svc.VerifyAndIssue(context.Background(), kp.Address(), sigStr, challenge)
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	// One-time use: using it again should fail
	_, err = svc.VerifyAndIssue(context.Background(), kp.Address(), sigStr, challenge)
	assert.ErrorIs(t, err, ErrChallengeExpired)
}

func TestAuthService_VerifyAndIssue_InvalidSignature(t *testing.T) {
	svc, kp := setupAuthService()

	challenge, err := svc.GenerateChallenge(context.Background(), kp.Address())
	require.NoError(t, err)

	// Use another keypair's signature
	randomKp, _ := keypair.Random()
	sigBytes, _ := randomKp.Sign([]byte(challenge))
	sigStr := base64.StdEncoding.EncodeToString(sigBytes)

	_, err = svc.VerifyAndIssue(context.Background(), kp.Address(), sigStr, challenge)
	assert.ErrorIs(t, err, ErrSignatureInvalid)
}

func TestAuthService_VerifyAndIssue_ExpiredChallenge(t *testing.T) {
	cfg := mockAuthConfig{
		secret:          "test-super-secret-key",
		tokenExpiry:     1 * time.Hour,
		// Instantly expire
		challengeExpiry: -1 * time.Second,
	}
	repo := &mockAuthUserRepository{users: make(map[string]*user.User)}
	svc := NewAuthService(NewUserService(repo), cfg)

	kp, _ := keypair.Random()
	challenge, _ := svc.GenerateChallenge(context.Background(), kp.Address())

	sigBytes, _ := kp.Sign([]byte(challenge))
	sigStr := base64.StdEncoding.EncodeToString(sigBytes)

	_, err := svc.VerifyAndIssue(context.Background(), kp.Address(), sigStr, challenge)
	assert.ErrorIs(t, err, ErrChallengeExpired)
}
