package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsschedule"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type mockSavingsScheduleService struct {
	schedules map[uuid.UUID]savingsschedule.SavingsSchedule
}

func (m *mockSavingsScheduleService) Create(_ context.Context, userID, goalID uuid.UUID, in service.CreateSavingsScheduleInput) (savingsschedule.SavingsSchedule, error) {
	for _, s := range m.schedules {
		if s.GoalID == goalID && s.IsActive {
			return savingsschedule.SavingsSchedule{}, savingsschedule.ErrActiveScheduleExists
		}
	}
	s := savingsschedule.SavingsSchedule{
		ID:        uuid.New(),
		UserID:    userID,
		GoalID:    goalID,
		VaultID:   in.VaultID,
		Amount:    in.Amount,
		Currency:  in.Currency,
		Frequency: savingsschedule.Frequency(in.Frequency),
		NextRunAt: time.Now().UTC(),
		IsActive:  true,
	}
	m.schedules[s.ID] = s
	return s, nil
}

func (m *mockSavingsScheduleService) List(_ context.Context, userID, goalID uuid.UUID) ([]savingsschedule.SavingsSchedule, error) {
	var out []savingsschedule.SavingsSchedule
	for _, s := range m.schedules {
		if s.UserID == userID && s.GoalID == goalID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *mockSavingsScheduleService) Cancel(_ context.Context, userID, goalID, scheduleID uuid.UUID) error {
	s, ok := m.schedules[scheduleID]
	if !ok || s.UserID != userID || s.GoalID != goalID {
		return savingsschedule.ErrScheduleNotFound
	}
	s.IsActive = false
	m.schedules[scheduleID] = s
	return nil
}

func TestSavingsScheduleHandler_CreateListCancel(t *testing.T) {
	userID := uuid.New()
	goalID := uuid.New()
	vaultID := uuid.New()
	svc := &mockSavingsScheduleService{schedules: make(map[uuid.UUID]savingsschedule.SavingsSchedule)}
	h := NewSavingsScheduleHandler(svc)

	mux := http.NewServeMux()
	h.Register(mux)
	handler := middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(
		withAuthUser(mux, userID),
	)
	server := httptest.NewServer(handler)
	defer server.Close()

	createBody := `{"amount":50,"currency":"USDC","frequency":"weekly","vault_id":"` + vaultID.String() + `"}`
	resp, err := http.Post(server.URL+"/api/v1/users/savings-goals/"+goalID.String()+"/schedules", "application/json", bytes.NewBufferString(createBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var envelope response.Response
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatal(err)
	}
	var created savingsschedule.SavingsSchedule
	if err := json.Unmarshal(data, &created); err != nil {
		t.Fatal(err)
	}

	listResp, err := http.Get(server.URL + "/api/v1/users/savings-goals/" + goalID.String() + "/schedules")
	if err != nil {
		t.Fatal(err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listResp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/users/savings-goals/"+goalID.String()+"/schedules/"+created.ID.String(), nil)
	delResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", delResp.StatusCode)
	}
}

func TestSavingsScheduleHandler_Create_Conflict(t *testing.T) {
	userID := uuid.New()
	goalID := uuid.New()
	vaultID := uuid.New()
	existingID := uuid.New()
	svc := &mockSavingsScheduleService{schedules: map[uuid.UUID]savingsschedule.SavingsSchedule{
		existingID: {
			ID:       existingID,
			UserID:   userID,
			GoalID:   goalID,
			IsActive: true,
		},
	}}
	h := NewSavingsScheduleHandler(svc)

	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(withAuthUser(mux, userID))
	defer server.Close()

	createBody := `{"amount":50,"currency":"USDC","frequency":"weekly","vault_id":"` + vaultID.String() + `"}`
	resp, err := http.Post(server.URL+"/api/v1/users/savings-goals/"+goalID.String()+"/schedules", "application/json", bytes.NewBufferString(createBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("create status = %d, want 409", resp.StatusCode)
	}
}
