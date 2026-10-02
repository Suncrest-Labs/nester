package handler

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
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
	if patch.Timezone != nil {
		u.Timezone = *patch.Timezone
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

	// Get by unknown wallet
	resp4, err := http.Get(server.URL + "/api/v1/users/wallet/G-DOES-NOT-EXIST")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp4.Body.Close()
	if resp4.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d", resp4.StatusCode)
	}
}

func TestUserHandler_Register_RejectsMalformedBody(t *testing.T) {
	repo := newMockUserRepository()
	svc := service.NewUserService(repo)
	handler := NewUserHandler(svc)

	mux := http.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	// decodeJSON rejects unknown fields and trailing content, on top of the
	// validator's required-field checks already covered above.
	body := bytes.NewBufferString(`{"wallet_address":"G-EXTRA","display_name":"X","unexpected_field":true}`)
	resp, err := http.Post(server.URL+"/api/v1/users", "application/json", body)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for unknown field, got %d", resp.StatusCode)
	}

	trailing := bytes.NewBufferString(`{"wallet_address":"G-TRAIL","display_name":"X"}{}`)
	respTrailing, err := http.Post(server.URL+"/api/v1/users", "application/json", trailing)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer respTrailing.Body.Close()
	if respTrailing.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for trailing content, got %d", respTrailing.StatusCode)
	}
}

func TestUserHandler_UpdateProfile(t *testing.T) {
	repo := newMockUserRepository()
	svc := service.NewUserService(repo)
	handler := NewUserHandler(svc)

	mux := http.NewServeMux()
	handler.Register(mux)

	u, err := svc.RegisterUser(context.Background(), "G-UPDATE-ME", "Bob")
	if err != nil {
		t.Fatalf("seed registration failed: %v", err)
	}

	authed := func(req *http.Request) *http.Request {
		return req.WithContext(auth.NewContext(req.Context(), auth.User{ID: u.ID.String()}))
	}

	t.Run("updates the caller's own profile", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/profile", bytes.NewBufferString(`{"savings_goal":"emergency fund"}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(req))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("rejects an invalid risk_profile value", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/profile", bytes.NewBufferString(`{"risk_profile":"yolo"}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(req))

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("accepts risk_profile case- and whitespace-insensitively", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/profile", bytes.NewBufferString(`{"risk_profile":" Aggressive "}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(req))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("requires authentication", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/profile", bytes.NewBufferString(`{"savings_goal":"x"}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req) // no auth context attached

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized, got %d", rec.Code)
		}
	})
}

func TestUserHandler_GetProfile(t *testing.T) {
	repo := newMockUserRepository()
	svc := service.NewUserService(repo)
	handler := NewUserHandler(svc)

	mux := http.NewServeMux()
	handler.Register(mux)

	u, err := svc.RegisterUser(context.Background(), "G-PROFILE-VIEW", "Carol")
	if err != nil {
		t.Fatalf("seed registration failed: %v", err)
	}

	t.Run("returns the caller's profile", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/users/profile", nil)
		req = req.WithContext(auth.NewContext(req.Context(), auth.User{ID: u.ID.String()}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("requires authentication", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/users/profile", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized, got %d", rec.Code)
		}
	})
}
