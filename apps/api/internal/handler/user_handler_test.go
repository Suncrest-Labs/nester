package handler

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/crypto"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/objectstorage"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

type mockUserRepository struct {
	users map[uuid.UUID]*user.User
}

func newMockUserRepository() *mockUserRepository {
	return &mockUserRepository{
		users: make(map[uuid.UUID]*user.User),
	}
}

func (m *mockUserRepository) Create(ctx context.Context, u *user.User) error {
	for _, existing := range m.users {
		if existing.WalletAddress == u.WalletAddress {
			return user.ErrDuplicateWallet
		}
	}
	m.users[u.ID] = u
	return nil
}

func (m *mockUserRepository) GetByID(ctx context.Context, id uuid.UUID) (*user.User, error) {
	if u, exists := m.users[id]; exists {
		return u, nil
	}
	return nil, user.ErrUserNotFound
}

func (m *mockUserRepository) GetByWalletAddress(ctx context.Context, addr string) (*user.User, error) {
	for _, u := range m.users {
		if u.WalletAddress == addr {
			return u, nil
		}
	}
	return nil, user.ErrUserNotFound
}

func (m *mockUserRepository) GetRoles(_ context.Context, _ uuid.UUID) ([]string, error) {
	return []string{}, nil
}

func (m *mockUserRepository) SaveKYCDocument(_ context.Context, _ *user.KYCDocument, _ *user.EncryptedKYCDoc) error {
	return nil
}

func (m *mockUserRepository) GetKYCDocument(_ context.Context, _ uuid.UUID) (*user.KYCDocument, *user.EncryptedKYCDoc, error) {
	return nil, nil, user.ErrUserNotFound
}

func (m *mockUserRepository) UpdateKYCStatus(_ context.Context, userID uuid.UUID, status user.KYCStatus, reason *string, reviewedAt *time.Time) error {
	u, err := m.GetByID(context.Background(), userID)
	if err != nil {
		return err
	}
	u.KYCStatus = status
	u.KYCRejectionReason = reason
	u.KYCReviewedAt = reviewedAt
	m.users[userID] = u
	return nil
}

func (m *mockUserRepository) UpdateProfile(_ context.Context, id uuid.UUID, patch user.ProfilePatch) (*user.User, error) {
	u, err := m.GetByID(context.Background(), id)
	if err != nil {
		return nil, err
	}
	if patch.RiskProfile != nil {
		u.RiskProfile = patch.RiskProfile
	}
	if patch.SavingsGoal != nil {
		u.SavingsGoal = patch.SavingsGoal
	}
	if patch.OnboardingCompleted != nil {
		u.OnboardingCompleted = *patch.OnboardingCompleted
	}
	m.users[id] = u
	return u, nil
}

func TestUserHandler_Register(t *testing.T) {
	repo := newMockUserRepository()
	svc := service.NewUserService(repo)
	handler := NewUserHandler(svc)

	mux := http.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
	defer server.Close()

	// Valid format
	body := bytes.NewBufferString(`{"wallet_address":"G-WALLET-123","display_name":"Satoshi"}`)
	resp, err := http.Post(server.URL+"/api/v1/users", "application/json", body)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", resp.StatusCode)
	}

	// Invalid format (missing display_name)
	bodyInvalid := bytes.NewBufferString(`{"wallet_address":"G-WALLET-456"}`)
	respInvalid, err := http.Post(server.URL+"/api/v1/users", "application/json", bodyInvalid)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer respInvalid.Body.Close()

	if respInvalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", respInvalid.StatusCode)
	}

	// Duplicate wallet
	bodyDuplicate := bytes.NewBufferString(`{"wallet_address":"G-WALLET-123","display_name":"Nakamoto"}`)
	respDuplicate, err := http.Post(server.URL+"/api/v1/users", "application/json", bodyDuplicate)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer respDuplicate.Body.Close()

	if respDuplicate.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d", respDuplicate.StatusCode)
	}
}

func TestUserHandler_GetEndpoints(t *testing.T) {
	repo := newMockUserRepository()
	svc := service.NewUserService(repo)
	handler := NewUserHandler(svc)

	mux := http.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	u, _ := svc.RegisterUser(context.Background(), "G-FETCH-ME", "Alice")

	// Get by ID
	resp1, err := http.Get(server.URL + "/api/v1/users/" + u.ID.String())
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp1.StatusCode)
	}

	// Get by unknown ID
	resp2, err := http.Get(server.URL + "/api/v1/users/" + uuid.New().String())
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d", resp2.StatusCode)
	}

	// Get by wallet
	resp3, err := http.Get(server.URL + "/api/v1/users/wallet/G-FETCH-ME")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp3.StatusCode)
	}
}

// TestUserHandler_KYCSubmissionAndRetrieval verifies that KYC submission
// reaches the API endpoint and persists status for retrieval.
func TestUserHandler_KYCSubmissionAndRetrieval(t *testing.T) {
	repo := newMockUserRepository()

	// The KYC path encrypts the id number before persisting it, so the
	// service needs a cipher — without one the submission fails with a 500.
	cipher, err := crypto.NewAccountCipher(testCipherKey(7))
	if err != nil {
		t.Fatalf("NewAccountCipher() error = %v", err)
	}
	svc := service.NewUserService(repo).WithCipher(cipher)
	handler := NewUserHandler(svc)

	// A document store is required: the handler rejects an upload with 503
	// rather than accepting a KYC record that points at nothing (#1191).
	// Without this the route answers STORAGE_UNAVAILABLE and the submission
	// never reaches the service, which is correct behaviour, not a bug in
	// the endpoint.
	store, err := objectstorage.NewLocalDiskStore(t.TempDir(), 8<<20, KYCAllowedContentTypes)
	if err != nil {
		t.Fatalf("NewLocalDiskStore() error = %v", err)
	}
	handler.SetKYCStore(store)

	mux := http.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	// Create a user
	u, _ := svc.RegisterUser(context.Background(), "G-KYC-TEST", "Bob")

	// Submit KYC with a hand-built multipart form. formBody below carries
	// the payload; the bytes.Buffer this used to allocate was never written
	// to or read, which is what the compiler was objecting to.
	formBody := `--boundary
Content-Disposition: form-data; name="full_name"

Bob Smith
--boundary
Content-Disposition: form-data; name="date_of_birth"

1990-01-15
--boundary
Content-Disposition: form-data; name="country"

US
--boundary
Content-Disposition: form-data; name="id_type"

passport
--boundary
Content-Disposition: form-data; name="id_number"

12345678
--boundary
Content-Disposition: form-data; name="id_front"; filename="passport_front.jpg"
Content-Type: image/jpeg

[fake image data]
--boundary--`

	// Submit KYC
	req, err := http.NewRequest("POST", server.URL+"/api/v1/users/kyc/"+u.ID.String(), bytes.NewBufferString(formBody))
	if err != nil {
		t.Fatalf("NewRequest failed: %v", err)
	}
	req.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST KYC submission failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("expected 202 Accepted, got %d", resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)
		t.Logf("response body: %s", body)
	}

	// Retrieve KYC status
	getResp, err := http.Get(server.URL + "/api/v1/users/kyc/" + u.ID.String())
	if err != nil {
		t.Fatalf("GET KYC status failed: %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", getResp.StatusCode)
	}

	// Parse response and verify status is pending
	respBody, _ := io.ReadAll(getResp.Body)
	if !bytes.Contains(respBody, []byte("pending")) {
		t.Errorf("expected 'pending' status in response, got: %s", respBody)
	}
}
