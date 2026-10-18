package ws

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
)

const (
	// DefaultTicketTTL is the maximum lifetime of a single-use WebSocket ticket
	// (30 seconds or less per issue #1519).
	DefaultTicketTTL = 30 * time.Second

	// MaxTicketTTL caps any caller-supplied TTL at 30 seconds.
	MaxTicketTTL = 30 * time.Second

	ticketRedisKeyPrefix = "ws:ticket:"
	subprotocolPrefix    = "nester.auth."
)

var (
	// ErrInvalidOrExpiredTicket is returned when a WebSocket ticket does not
	// exist, has already been consumed, or has expired.
	ErrInvalidOrExpiredTicket = errors.New("websocket ticket is invalid, expired, or already used")

	// ErrQueryTokenJWTForbidden is returned when a client attempts to pass a
	// bearer JWT in the ?token= query parameter.
	ErrQueryTokenJWTForbidden = errors.New("bearer JWT in query string is forbidden; use a single-use ticket or Sec-WebSocket-Protocol")
)

type ticketRecord struct {
	UserID    string    `json:"user_id"`
	SessionID string    `json:"session_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// TicketStore issues and consumes short-lived single-use WebSocket tickets.
// It uses Redis when configured and falls back to an in-memory map for
// single-instance deployments and unit tests.
type TicketStore struct {
	redis *redis.Client
	ttl   time.Duration
	now   func() time.Time

	mu      sync.Mutex
	records map[string]ticketRecord
}

// NewTicketStore creates a TicketStore with a TTL capped at MaxTicketTTL (30s).
func NewTicketStore(rc *redis.Client, ttl time.Duration) *TicketStore {
	if ttl <= 0 || ttl > MaxTicketTTL {
		ttl = DefaultTicketTTL
	}
	return &TicketStore{
		redis:   rc,
		ttl:     ttl,
		now:     time.Now,
		records: make(map[string]ticketRecord),
	}
}

// Issue creates a single-use ticket bound to userID and sessionID.
func (s *TicketStore) Issue(ctx context.Context, userID, sessionID string) (string, time.Time, error) {
	if strings.TrimSpace(userID) == "" {
		return "", time.Time{}, errors.New("userID is required to issue websocket ticket")
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("generate ticket: %w", err)
	}
	ticket := hex.EncodeToString(raw)
	now := s.now()
	expiresAt := now.Add(s.ttl)

	rec := ticketRecord{
		UserID:    userID,
		SessionID: sessionID,
		ExpiresAt: expiresAt,
	}

	if s.redis != nil {
		payload, err := json.Marshal(rec)
		if err != nil {
			return "", time.Time{}, err
		}
		if err := s.redis.Set(ctx, ticketRedisKeyPrefix+ticket, payload, s.ttl).Err(); err != nil {
			return "", time.Time{}, fmt.Errorf("persist websocket ticket: %w", err)
		}
		return ticket, expiresAt, nil
	}

	s.mu.Lock()
	for k, existing := range s.records {
		if !now.Before(existing.ExpiresAt) {
			delete(s.records, k)
		}
	}
	s.records[ticket] = rec
	s.mu.Unlock()

	return ticket, expiresAt, nil
}

// Consume atomically retrieves and deletes a single-use ticket, returning the
// associated userID and sessionID if the ticket is valid and unexpired.
func (s *TicketStore) Consume(ctx context.Context, ticket string) (string, string, error) {
	ticket = strings.TrimSpace(ticket)
	if ticket == "" {
		return "", "", ErrInvalidOrExpiredTicket
	}
	now := s.now()

	if s.redis != nil {
		val, err := s.redis.GetDel(ctx, ticketRedisKeyPrefix+ticket).Result()
		if err != nil {
			return "", "", ErrInvalidOrExpiredTicket
		}
		var rec ticketRecord
		if err := json.Unmarshal([]byte(val), &rec); err != nil {
			return "", "", ErrInvalidOrExpiredTicket
		}
		if !now.Before(rec.ExpiresAt) {
			return "", "", ErrInvalidOrExpiredTicket
		}
		return rec.UserID, rec.SessionID, nil
	}

	s.mu.Lock()
	rec, ok := s.records[ticket]
	if ok {
		delete(s.records, ticket)
	}
	s.mu.Unlock()

	if !ok || !now.Before(rec.ExpiresAt) {
		return "", "", ErrInvalidOrExpiredTicket
	}
	return rec.UserID, rec.SessionID, nil
}

// HandleIssueTicket handles POST /api/v1/ws/ticket, issuing a single-use
// short-lived ticket for the authenticated caller.
func (h *Hub) HandleIssueTicket(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok || strings.TrimSpace(user.ID) == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":"UNAUTHORIZED","message":"authentication required"}}`))
		return
	}

	store := h.ensureTicketStore()
	ticket, expiresAt, err := store.Issue(r.Context(), user.ID, user.SessionID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":"INTERNAL_ERROR","message":"failed to issue websocket ticket"}}`))
		return
	}

	ttlSec := int(expiresAt.Sub(store.now()).Seconds())
	if ttlSec <= 0 {
		ttlSec = int(DefaultTicketTTL.Seconds())
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"data": map[string]any{
			"ticket":     ticket,
			"expires_at": expiresAt.UTC().Format(time.RFC3339),
			"expires_in": ttlSec,
		},
	})
}

func isJWTString(candidate string) bool {
	candidate = strings.TrimSpace(candidate)
	if strings.Count(candidate, ".") != 2 {
		return false
	}
	parts := strings.Split(candidate, ".")
	return len(parts) == 3 && len(parts[0]) > 0 && len(parts[1]) > 0 && len(parts[2]) > 0
}

func extractSubprotocolCredential(r *http.Request) (credential string, selectedProto string) {
	var hasNesterV1 bool
	var authProto string
	for _, rawHeader := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, part := range strings.Split(rawHeader, ",") {
			proto := strings.TrimSpace(part)
			if proto == "nester.v1" {
				hasNesterV1 = true
			}
			if strings.HasPrefix(proto, subprotocolPrefix) && credential == "" {
				cred := strings.TrimPrefix(proto, subprotocolPrefix)
				if cred != "" {
					credential = cred
					authProto = proto
				}
			}
		}
	}
	if credential == "" {
		return "", ""
	}
	if hasNesterV1 {
		return credential, "nester.v1"
	}
	return credential, authProto
}

