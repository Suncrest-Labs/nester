package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/crypto"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/objectstorage"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// recordingKYCRepository wraps mockUserRepository and captures the exact
// KYCDocument SaveKYCDocument was called with, so tests can assert on what
// was actually persisted rather than only on the HTTP response.
type recordingKYCRepository struct {
	*mockUserRepository
	mu    sync.Mutex
	saved *user.KYCDocument
}

func (r *recordingKYCRepository) SaveKYCDocument(ctx context.Context, doc *user.KYCDocument, encrypted *user.EncryptedKYCDoc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	saved := *doc
	r.saved = &saved
	return r.mockUserRepository.SaveKYCDocument(ctx, doc, encrypted)
}

func testCipherKey(b byte) string {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = b
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func newKYCTestHandler(t *testing.T, withStore bool) (*UserHandler, *recordingKYCRepository, uuid.UUID) {
	t.Helper()
	repo := &recordingKYCRepository{mockUserRepository: newMockUserRepository()}
	userID := uuid.New()
	repo.users[userID] = &user.User{ID: userID, WalletAddress: "G-KYC-TEST"}

	cipher, err := crypto.NewAccountCipher(testCipherKey(7))
	if err != nil {
		t.Fatalf("NewAccountCipher() error = %v", err)
	}
	svc := service.NewUserService(repo).WithCipher(cipher)
	h := NewUserHandler(svc)

	if withStore {
		store, err := objectstorage.NewLocalDiskStore(t.TempDir(), 8<<20, []string{"image/jpeg", "image/png", "application/pdf"})
		if err != nil {
			t.Fatalf("NewLocalDiskStore() error = %v", err)
		}
		h.SetKYCStore(store)
	}

	return h, repo, userID
}

func newTestServer(h *UserHandler) *httptest.Server {
	mux := http.NewServeMux()
	h.Register(mux)
	return httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
}

// buildKYCMultipartBody constructs a multipart/form-data body for the
// submitKYC endpoint. filename lets a test supply a hostile filename to
// verify it never influences the stored object key.
func buildKYCMultipartBody(t *testing.T, fields map[string]string, filename string, fileContent []byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("WriteField(%q) error = %v", k, err)
		}
	}
	if filename != "" {
		part, err := w.CreatePart(map[string][]string{
			"Content-Disposition": {`form-data; name="id_front"; filename="` + filename + `"`},
			"Content-Type":        {"image/jpeg"},
		})
		if err != nil {
			t.Fatalf("CreatePart() error = %v", err)
		}
		if _, err := part.Write(fileContent); err != nil {
			t.Fatalf("part.Write() error = %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("multipart writer Close() error = %v", err)
	}
	return &buf, w.FormDataContentType()
}

func TestSubmitKYC_FullSubmissionRoundTrips(t *testing.T) {
	h, repo, userID := newKYCTestHandler(t, true)
	server := newTestServer(h)
	defer server.Close()

	fields := map[string]string{
		"full_name":     "Ada Lovelace",
		"date_of_birth": "1990-05-14",
		"country":       "GB",
		"id_type":       "passport",
		"id_number":     "X12345678",
	}
	body, contentType := buildKYCMultipartBody(t, fields, "passport.jpg", []byte("fake jpeg bytes"))

	resp, err := http.Post(server.URL+"/api/v1/users/kyc/"+userID.String(), contentType, body)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", resp.StatusCode, respBody)
	}

	repo.mu.Lock()
	saved := repo.saved
	repo.mu.Unlock()
	if saved == nil {
		t.Fatal("expected SaveKYCDocument to have been called")
	}

	// nester#1190: every identity field round-trips, none silently dropped.
	if saved.FullName != "Ada Lovelace" {
		t.Fatalf("FullName = %q, want %q", saved.FullName, "Ada Lovelace")
	}
	if saved.Country != "GB" {
		t.Fatalf("Country = %q, want %q", saved.Country, "GB")
	}
	if saved.DateOfBirth.Format("2006-01-02") != "1990-05-14" {
		t.Fatalf("DateOfBirth = %v, want 1990-05-14", saved.DateOfBirth)
	}

	// nester#1191: the stored key is server-generated, never the client's
	// literal filename or a mock placeholder.
	if strings.Contains(saved.FrontObjectKey, "passport.jpg") {
		t.Fatalf("FrontObjectKey %q must not contain the client-supplied filename", saved.FrontObjectKey)
	}
	if strings.HasPrefix(saved.FrontObjectKey, "s3://mock-bucket/") {
		t.Fatalf("FrontObjectKey %q still looks like the old mock placeholder", saved.FrontObjectKey)
	}
	if saved.FrontObjectKey == "" {
		t.Fatal("expected a non-empty stored key")
	}
}

func TestSubmitKYC_HostileFilenameDoesNotInfluenceTheStoredKey(t *testing.T) {
	h, repo, userID := newKYCTestHandler(t, true)
	server := newTestServer(h)
	defer server.Close()

	fields := map[string]string{
		"full_name": "Grace Hopper", "date_of_birth": "1985-01-01", "country": "US",
		"id_type": "passport", "id_number": "Y98765",
	}
	body, contentType := buildKYCMultipartBody(t, fields, "../../../etc/passwd", []byte("x"))

	resp, err := http.Post(server.URL+"/api/v1/users/kyc/"+userID.String(), contentType, body)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 202 Accepted, got %d: %s", resp.StatusCode, respBody)
	}

	repo.mu.Lock()
	saved := repo.saved
	repo.mu.Unlock()
	if saved == nil {
		t.Fatal("expected SaveKYCDocument to have been called")
	}
	if strings.Contains(saved.FrontObjectKey, "..") || strings.Contains(saved.FrontObjectKey, "passwd") {
		t.Fatalf("stored key %q was influenced by the hostile filename", saved.FrontObjectKey)
	}
}

func TestSubmitKYC_MissingFullNameIsRejected(t *testing.T) {
	h, _, userID := newKYCTestHandler(t, true)
	server := newTestServer(h)
	defer server.Close()

	fields := map[string]string{
		"date_of_birth": "1990-05-14", "country": "GB", "id_type": "passport", "id_number": "X1",
	}
	body, contentType := buildKYCMultipartBody(t, fields, "id.jpg", []byte("x"))

	resp, err := http.Post(server.URL+"/api/v1/users/kyc/"+userID.String(), contentType, body)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for a missing full_name, got %d", resp.StatusCode)
	}
}

func TestSubmitKYC_InvalidDateOfBirthIsRejected(t *testing.T) {
	h, _, userID := newKYCTestHandler(t, true)
	server := newTestServer(h)
	defer server.Close()

	fields := map[string]string{
		"full_name": "A B", "date_of_birth": "not-a-date", "country": "GB", "id_type": "passport", "id_number": "X1",
	}
	body, contentType := buildKYCMultipartBody(t, fields, "id.jpg", []byte("x"))

	resp, err := http.Post(server.URL+"/api/v1/users/kyc/"+userID.String(), contentType, body)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for a malformed date_of_birth, got %d", resp.StatusCode)
	}
}

func TestSubmitKYC_UnknownCountryCodeIsRejected(t *testing.T) {
	h, _, userID := newKYCTestHandler(t, true)
	server := newTestServer(h)
	defer server.Close()

	fields := map[string]string{
		"full_name": "A B", "date_of_birth": "1990-05-14", "country": "ZZ", "id_type": "passport", "id_number": "X1",
	}
	body, contentType := buildKYCMultipartBody(t, fields, "id.jpg", []byte("x"))

	resp, err := http.Post(server.URL+"/api/v1/users/kyc/"+userID.String(), contentType, body)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for an unknown country code, got %d", resp.StatusCode)
	}
}

func TestSubmitKYC_RejectsUploadWhenStorageIsNotConfigured(t *testing.T) {
	h, repo, userID := newKYCTestHandler(t, false) // no SetKYCStore call
	server := newTestServer(h)
	defer server.Close()

	fields := map[string]string{
		"full_name": "A B", "date_of_birth": "1990-05-14", "country": "GB", "id_type": "passport", "id_number": "X1",
	}
	body, contentType := buildKYCMultipartBody(t, fields, "id.jpg", []byte("x"))

	resp, err := http.Post(server.URL+"/api/v1/users/kyc/"+userID.String(), contentType, body)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	// nester#1191: storage not ready => reject, never accept-and-discard.
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable when storage is unset, got %d", resp.StatusCode)
	}
	repo.mu.Lock()
	saved := repo.saved
	repo.mu.Unlock()
	if saved != nil {
		t.Fatal("expected no KYC document to be persisted when storage is unavailable")
	}
}

func TestSubmitKYC_RejectsDisallowedContentType(t *testing.T) {
	h, _, userID := newKYCTestHandler(t, true)
	server := newTestServer(h)
	defer server.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range map[string]string{
		"full_name": "A B", "date_of_birth": "1990-05-14", "country": "GB", "id_type": "passport", "id_number": "X1",
	} {
		_ = w.WriteField(k, v)
	}
	part, _ := w.CreatePart(map[string][]string{
		"Content-Disposition": {`form-data; name="id_front"; filename="script.sh"`},
		"Content-Type":        {"application/x-sh"},
	})
	_, _ = part.Write([]byte("#!/bin/sh\necho hi"))
	_ = w.Close()

	resp, err := http.Post(server.URL+"/api/v1/users/kyc/"+userID.String(), w.FormDataContentType(), &buf)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415 Unsupported Media Type, got %d", resp.StatusCode)
	}
}

func TestSubmitKYC_ResponseBodyDoesNotLeakTheStorageKey(t *testing.T) {
	// The response only ever needs to tell the client the submission is
	// pending — no reason to echo storage internals back.
	h, _, userID := newKYCTestHandler(t, true)
	server := newTestServer(h)
	defer server.Close()

	fields := map[string]string{
		"full_name": "A B", "date_of_birth": "1990-05-14", "country": "GB", "id_type": "passport", "id_number": "X1",
	}
	body, contentType := buildKYCMultipartBody(t, fields, "id.jpg", []byte("x"))

	resp, err := http.Post(server.URL+"/api/v1/users/kyc/"+userID.String(), contentType, body)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	var parsed map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := parsed["front_object_key"]; ok {
		t.Fatal("response body must not include the storage key")
	}
}
