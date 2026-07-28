package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/backfill"
	"github.com/suncrestlabs/nester/apps/api/internal/stellar"
)

type stubBackfillTrigger struct {
	startRun  *backfill.Run
	startErr  error
	resumeRun *backfill.Run
	resumeErr error
	lastStart stellar.StartInput
}

func (s *stubBackfillTrigger) Start(_ context.Context, in stellar.StartInput) (*backfill.Run, error) {
	s.lastStart = in
	return s.startRun, s.startErr
}
func (s *stubBackfillTrigger) Resume(context.Context, uuid.UUID) (*backfill.Run, error) {
	return s.resumeRun, s.resumeErr
}

type stubBackfillRunLister struct {
	byID map[uuid.UUID]*backfill.Run
	list []backfill.Run
	err  error
}

func (s *stubBackfillRunLister) GetByID(_ context.Context, id uuid.UUID) (*backfill.Run, error) {
	if s.err != nil {
		return nil, s.err
	}
	run, ok := s.byID[id]
	if !ok {
		return nil, backfill.ErrRunNotFound
	}
	return run, nil
}
func (s *stubBackfillRunLister) List(context.Context, int) ([]backfill.Run, error) {
	return s.list, s.err
}

func newAdminHandlerForBackfillTest() (*AdminHandler, *stubBackfillTrigger, *stubBackfillRunLister) {
	h := NewAdminHandler(newAdminHandlerStubService(uuid.New()), nil)
	trigger := &stubBackfillTrigger{}
	runs := &stubBackfillRunLister{byID: map[uuid.UUID]*backfill.Run{}}
	h.SetBackfillRunner(trigger, runs)
	return h, trigger, runs
}

func TestAdminHandler_StartBackfill_RequiresInitiatedBy(t *testing.T) {
	h, _, _ := newAdminHandlerForBackfillTest()
	mux := http.NewServeMux()
	h.Register(mux)

	body, _ := json.Marshal(map[string]any{"from_ledger": 100, "to_ledger": 200})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/backfill", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminHandler_StartBackfill_RejectsInvalidMode(t *testing.T) {
	h, _, _ := newAdminHandlerForBackfillTest()
	mux := http.NewServeMux()
	h.Register(mux)

	body, _ := json.Marshal(map[string]any{"from_ledger": 100, "to_ledger": 200, "initiated_by": "op", "mode": "delete_everything"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/backfill", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminHandler_StartBackfill_Success(t *testing.T) {
	h, trigger, _ := newAdminHandlerForBackfillTest()
	trigger.startRun = &backfill.Run{ID: uuid.New(), FromLedger: 100, ToLedger: 200, Status: backfill.StatusCompleted}
	mux := http.NewServeMux()
	h.Register(mux)

	body, _ := json.Marshal(map[string]any{
		"from_ledger": 100, "to_ledger": 200, "initiated_by": "operator-1", "contract_ids": []string{"C1"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/backfill", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if trigger.lastStart.InitiatedBy != "operator-1" {
		t.Errorf("expected initiated_by to be passed through, got %q", trigger.lastStart.InitiatedBy)
	}
	if trigger.lastStart.Mode != backfill.ModeBackfill {
		t.Errorf("expected mode to default to backfill, got %q", trigger.lastStart.Mode)
	}
}

func TestAdminHandler_StartBackfill_PropagatesRunnerError(t *testing.T) {
	h, trigger, _ := newAdminHandlerForBackfillTest()
	trigger.startErr = stellar.ErrTooCloseToHead
	mux := http.NewServeMux()
	h.Register(mux)

	body, _ := json.Marshal(map[string]any{"from_ledger": 100, "to_ledger": 200, "initiated_by": "op"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/backfill", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminHandler_GetBackfillRun_NotFound(t *testing.T) {
	h, _, _ := newAdminHandlerForBackfillTest()
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/backfill/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestAdminHandler_GetBackfillRun_ReturnsRun(t *testing.T) {
	h, _, runs := newAdminHandlerForBackfillTest()
	id := uuid.New()
	runs.byID[id] = &backfill.Run{ID: id, Status: backfill.StatusRunning, EventsProcessed: 42}
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/backfill/"+id.String(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	data := out["data"].(map[string]any)
	if data["events_processed"].(float64) != 42 {
		t.Errorf("expected events_processed=42, got %v", data["events_processed"])
	}
}

func TestAdminHandler_ListBackfillRuns_ReturnsEmptyArrayNotNull(t *testing.T) {
	h, _, _ := newAdminHandlerForBackfillTest()
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/backfill", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	if _, ok := out["data"].([]any); !ok {
		t.Errorf("expected data to be a JSON array, got %T: %v", out["data"], out["data"])
	}
}

func TestAdminHandler_ResumeBackfill_RejectsInvalidID(t *testing.T) {
	h, _, _ := newAdminHandlerForBackfillTest()
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/backfill/not-a-uuid/resume", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAdminHandler_ResumeBackfill_Success(t *testing.T) {
	h, trigger, _ := newAdminHandlerForBackfillTest()
	trigger.resumeRun = &backfill.Run{ID: uuid.New(), Status: backfill.StatusCompleted}
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/backfill/"+uuid.New().String()+"/resume", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminHandler_Backfill_NotConfiguredByDefault(t *testing.T) {
	// A handler that never called SetBackfillRunner must fail closed with a
	// clear error, not panic on a nil interface.
	h := NewAdminHandler(newAdminHandlerStubService(uuid.New()), nil)
	mux := http.NewServeMux()
	h.Register(mux)

	body, _ := json.Marshal(map[string]any{"from_ledger": 100, "to_ledger": 200, "initiated_by": "op"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/backfill", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 (not configured, not panic), got %d: %s", rec.Code, rec.Body.String())
	}
}
