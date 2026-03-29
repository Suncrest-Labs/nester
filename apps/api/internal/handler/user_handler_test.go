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
	respInvalid, _ := http.Post(server.URL+"/api/v1/users", "application/json", bodyInvalid)
	defer respInvalid.Body.Close()

	if respInvalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", respInvalid.StatusCode)
	}

	// Duplicate wallet
	bodyDuplicate := bytes.NewBufferString(`{"wallet_address":"G-WALLET-123","display_name":"Nakamoto"}`)
	respDuplicate, _ := http.Post(server.URL+"/api/v1/users", "application/json", bodyDuplicate)
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
	resp1, _ := http.Get(server.URL + "/api/v1/users/" + u.ID.String())
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp1.StatusCode)
	}

	// Get by unknown ID
	resp2, _ := http.Get(server.URL + "/api/v1/users/" + uuid.New().String())
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d", resp2.StatusCode)
	}

	// Get by wallet
	resp3, _ := http.Get(server.URL + "/api/v1/users/wallet/G-FETCH-ME")
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp3.StatusCode)
	}
}
