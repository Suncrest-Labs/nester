package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type RiskEvaluationRequest struct {
	UserID             string  `json:"user_id"`
	Amount             float64 `json:"amount"`
	HistoryAvg         float64 `json:"history_avg"`
	Velocity24h        int     `json:"velocity_24h"`
	IsNovelAccount     bool    `json:"is_novel_account"`
	DestinationAccount string  `json:"destination_account"`
}

type RiskEvaluationResponse struct {
	Score             int      `json:"score"`
	TriggeredRules    []string `json:"triggered_rules"`
	RecommendedAction string   `json:"recommended_action"`
	Explanation       string   `json:"explanation"`
}

type RiskClient struct {
	url    string
	client *http.Client
}

func NewRiskClient(url string) *RiskClient {
	return &RiskClient{
		url:    url,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *RiskClient) Evaluate(ctx context.Context, req RiskEvaluationRequest) (RiskEvaluationResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return RiskEvaluationResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.url+"/api/v1/risk/evaluate", bytes.NewBuffer(body))
	if err != nil {
		return RiskEvaluationResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return RiskEvaluationResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return RiskEvaluationResponse{}, fmt.Errorf("risk service returned status %d", resp.StatusCode)
	}

	var result RiskEvaluationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return RiskEvaluationResponse{}, err
	}

	return result, nil
}
