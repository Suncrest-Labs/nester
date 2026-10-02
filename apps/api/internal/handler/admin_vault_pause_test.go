package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/moneypath"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// fakeVaultPauseService is an in-memory VaultMoneyPathSwitchService so the
// handler can be tested without a database.
type fakeVaultPauseService struct {
	switches map[moneypath.VaultSwitchKey]moneypath.VaultSwitch
	listErr  error
	setErr   error
}

func newFakeVaultPauseService() *fakeVaultPauseService {
	return &fakeVaultPauseService{switches: make(map[moneypath.VaultSwitchKey]moneypath.VaultSwitch)}
}

func (f *fakeVaultPauseService) List(_ context.Context, vaultID uuid.UUID) ([]moneypath.VaultSwitch, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]moneypath.VaultSwitch, 0, len(moneypath.Operations()))
	for _, op := range moneypath.Operations() {
		key := moneypath.VaultSwitchKey{VaultID: vaultID, Operation: op}
		if s, ok := f.switches[key]; ok {
			out = append(out, s)
			continue
		}
		out = append(out, moneypath.VaultSwitch{VaultID: vaultID, Operation: op})
	}
	return out, nil
}

func (f *fakeVaultPauseService) SetPaused(_ context.Context, vaultID uuid.UUID, op moneypath.Operation, paused bool, reason string, actor *uuid.UUID, _ string) (moneypath.VaultSwitch, error) {
	if f.setErr != nil {
		return moneypath.VaultSwitch{}, f.setErr
	}
	s := moneypath.VaultSwitch{
		VaultID:   vaultID,
		Operation: op,
		Paused:    paused,
		Reason:    reason,
		ChangedBy: actor,
		UpdatedAt: time.Now().UTC(),
	}
	f.switches[s.Key()] = s
	return s, nil
}

func newVaultPauseMux(t *testing.T, svc VaultMoneyPathSwitchService) (*http.ServeMux, uuid.UUID) {
	t.Helper()
	vaultID := uuid.New()
	h := NewAdminHandler(newAdminHandlerStubService(vaultID), nil)
	if svc != nil {
		h.SetVaultMoneyPathSwitches(svc)
	}
	mux := http.NewServeMux()
	h.Register(mux)
	return mux, vaultID
}

func decodeVaultPauseData[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var env response.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (body=%s)", err, rec.Body.String())
	}
	raw, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatalf("re-marshal data: %v", err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode data: %v (raw=%s)", err, raw)
	}
	return out
}

// The list route must report both operations even when neither has a stored
// row, so the admin view can render a switch nobody has touched.
func TestListVaultPauseSwitchesReportsBothOperations(t *testing.T) {
	mux, vaultID := newVaultPauseMux(t, newFakeVaultPauseService())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/vaults/"+vaultID.String()+"/money-path/switches", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	switches := decodeVaultPauseData[[]vaultPauseSwitchResponse](t, rec)
	if len(switches) != len(moneypath.Operations()) {
		t.Fatalf("got %d switches, want %d", len(switches), len(moneypath.Operations()))
	}
	for _, s := range switches {
		if s.Paused {
			t.Errorf("%s reported paused on an untouched vault", s.Operation)
		}
		if s.VaultID != vaultID.String() {
			t.Errorf("%s reported vault %s, want %s", s.Operation, s.VaultID, vaultID)
		}
	}
}

// Setting one switch must persist and be reported back, and must not touch
// the other operation on the same vault.
func TestSetVaultPauseSwitchEngagesOneOperation(t *testing.T) {
	svc := newFakeVaultPauseService()
	mux, vaultID := newVaultPauseMux(t, svc)

	body := bytes.NewBufferString(`{"paused":true,"reason":"draining for migration"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/vaults/"+vaultID.String()+"/money-path/switches/deposit", body)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	got := decodeVaultPauseData[vaultPauseSwitchResponse](t, rec)
	if !got.Paused {
		t.Fatalf("response reports paused=false, want true")
	}
	if got.Operation != string(moneypath.OperationDeposit) {
		t.Fatalf("operation = %q, want deposit", got.Operation)
	}
	if got.Reason != "draining for migration" {
		t.Fatalf("reason = %q", got.Reason)
	}

	withdrawal := svc.switches[moneypath.VaultSwitchKey{VaultID: vaultID, Operation: moneypath.OperationWithdrawal}]
	if withdrawal.Paused {
		t.Fatal("pausing deposits must not pause withdrawals on the same vault")
	}
}

func TestSetVaultPauseSwitchRejectsUnknownOperation(t *testing.T) {
	mux, vaultID := newVaultPauseMux(t, newFakeVaultPauseService())

	body := bytes.NewBufferString(`{"paused":true}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/vaults/"+vaultID.String()+"/money-path/switches/transfer", body)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

// A body that omits "paused" must be rejected, not treated as a release:
// silently releasing a switch engaged during an incident is the failure mode
// the required field exists to prevent.
func TestSetVaultPauseSwitchRequiresPausedField(t *testing.T) {
	mux, vaultID := newVaultPauseMux(t, newFakeVaultPauseService())

	body := bytes.NewBufferString(`{"reason":"no flag"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/vaults/"+vaultID.String()+"/money-path/switches/deposit", body)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestSetVaultPauseSwitchRejectsMalformedVaultID(t *testing.T) {
	mux, _ := newVaultPauseMux(t, newFakeVaultPauseService())

	body := bytes.NewBufferString(`{"paused":true}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/vaults/not-a-uuid/money-path/switches/deposit", body)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

// A deployment with no switch service wired must say so rather than report
// every vault as open or fall through to a 500.
func TestVaultPauseRoutesReportNotConfigured(t *testing.T) {
	mux, vaultID := newVaultPauseMux(t, nil)

	for _, tc := range []struct {
		name   string
		method string
		target string
		body   string
	}{
		{"list", http.MethodGet, "/api/v1/admin/vaults/" + vaultID.String() + "/money-path/switches", ""},
		{"set", http.MethodPut, "/api/v1/admin/vaults/" + vaultID.String() + "/money-path/switches/deposit", `{"paused":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("got %d, want 503 (body=%s)", rec.Code, rec.Body.String())
			}
			var env response.Response
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			if env.Error == nil || env.Error.Code != "VAULT_PAUSE_NOT_CONFIGURED" {
				t.Fatalf("unexpected error body: %+v", env.Error)
			}
		})
	}
}

// The service surfaces read failures; the handler must not swallow them into
// a false "everything is open" response.
func TestListVaultPauseSwitchesPropagatesErrors(t *testing.T) {
	svc := newFakeVaultPauseService()
	svc.listErr = errors.New("database down")
	mux, vaultID := newVaultPauseMux(t, svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/vaults/"+vaultID.String()+"/money-path/switches", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500 (body=%s)", rec.Code, rec.Body.String())
	}
}
