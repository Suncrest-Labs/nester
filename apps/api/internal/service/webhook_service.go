package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/webhook"
)

type WebhookService struct {
	repo webhook.Repository
}

func NewWebhookService(repo webhook.Repository) *WebhookService {
	return &WebhookService{repo: repo}
}

type RegisterWebhookInput struct {
	URL    string `json:"url"`
	Secret string `json:"secret"`
}

func (s *WebhookService) Register(ctx context.Context, userID uuid.UUID, in RegisterWebhookInput) (webhook.Webhook, error) {
	u := strings.TrimSpace(in.URL)
	if u == "" {
		return webhook.Webhook{}, fmt.Errorf("%w: url is required", webhook.ErrInvalidWebhook)
	}
	wh := &webhook.Webhook{
		ID:     uuid.New(),
		UserID: userID,
		URL:    u,
		Secret: in.Secret,
	}
	if err := s.repo.Create(ctx, wh); err != nil {
		return webhook.Webhook{}, err
	}
	return *wh, nil
}

func (s *WebhookService) List(ctx context.Context, userID uuid.UUID) ([]webhook.Webhook, error) {
	return s.repo.ListByUser(ctx, userID)
}

func (s *WebhookService) Delete(ctx context.Context, userID, id uuid.UUID) error {
	return s.repo.Delete(ctx, id, userID)
}

// FireForUser fetches all webhooks registered by userID and delivers the payload to each.
// event is the event name (e.g. "goal.milestone.50"). payload is the JSON body bytes.
// Delivery failures are retried up to 3 times with exponential backoff (1s, 2s, 4s).
func (s *WebhookService) FireForUser(ctx context.Context, userID uuid.UUID, event string, payload []byte) {
	hooks, err := s.repo.ListByUser(ctx, userID)
	if err != nil || len(hooks) == 0 {
		return
	}
	for _, wh := range hooks {
		go deliverWebhook(wh, event, payload)
	}
}

func deliverWebhook(wh webhook.Webhook, _ string, payload []byte) {
	const maxAttempts = 3
	backoff := time.Second
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(backoff)
			backoff *= 2
		}
		if deliver(wh, payload) {
			return
		}
	}
}

func deliver(wh webhook.Webhook, payload []byte) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(payload))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	if wh.Secret != "" {
		req.Header.Set("X-Nester-Signature", signPayload([]byte(wh.Secret), payload))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func signPayload(secret, payload []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// BuildWebhookPayload constructs the standard JSON payload for webhook delivery.
func BuildWebhookPayload(event string, goalID, userID uuid.UUID, milestonePct int, currentAmount, targetAmount string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"event":          event,
		"goal_id":        goalID.String(),
		"user_id":        userID.String(),
		"milestone_pct":  milestonePct,
		"current_amount": currentAmount,
		"target_amount":  targetAmount,
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
	})
}
