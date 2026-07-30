package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/intelligence"
)

func TestPrometheusClient_GetGoalCoaching(t *testing.T) {
	var gotPath string
	var gotBody intelligence.CoachingRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(intelligence.CoachingResponse{
			ProgressAssessment: "You're on track.",
			Nudges:             []string{"Nice work"},
			Confidence:         "high",
		})
	}))
	defer server.Close()

	client := NewPrometheusClient(PrometheusConfig{BaseURL: server.URL, Timeout: 2 * time.Second})

	resp, err := client.GetGoalCoaching(t.Context(), intelligence.CoachingRequest{
		Goal: intelligence.SavingsGoalContext{
			TargetAmount:  1000,
			Currency:      "USDC",
			CurrentAmount: 340,
			ProgressPct:   34,
		},
	})
	if err != nil {
		t.Fatalf("GetGoalCoaching() error = %v", err)
	}
	if gotPath != "/intelligence/coaching" {
		t.Errorf("path = %q, want /intelligence/coaching", gotPath)
	}
	if gotBody.Goal.Currency != "USDC" {
		t.Errorf("request body not forwarded correctly: %+v", gotBody)
	}
	if resp.ProgressAssessment != "You're on track." {
		t.Errorf("ProgressAssessment = %q", resp.ProgressAssessment)
	}
}

func TestPrometheusClient_GetGoalCoaching_UpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewPrometheusClient(PrometheusConfig{BaseURL: server.URL, Timeout: 2 * time.Second})

	_, err := client.GetGoalCoaching(t.Context(), intelligence.CoachingRequest{})
	if err == nil {
		t.Fatal("expected error on upstream 500, got nil")
	}
}
