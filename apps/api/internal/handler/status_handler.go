package handler

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/moneypath"
	"github.com/suncrestlabs/nester/apps/api/internal/freshness"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

const defaultStatusCacheTTL = 5 * time.Second

// PublicStatusPayload is the public degraded-mode status snapshot returned by
// GET /api/v1/status (#1516).
type PublicStatusPayload struct {
	Status            string    `json:"status"`
	DepositsPaused    bool      `json:"deposits_paused"`
	WithdrawalsPaused bool      `json:"withdrawals_paused"`
	PauseReason       string    `json:"pause_reason,omitempty"`
	IndexerLagSeconds int64     `json:"indexer_lag_seconds"`
	IndexerLagLedgers uint64    `json:"indexer_lag_ledgers"`
	IndexerStale      bool      `json:"indexer_stale"`
	RPCHealthy        bool      `json:"rpc_healthy"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// StatusHandler serves GET /api/v1/status with short in-memory caching so
// dApp polling cannot amplify load on Postgres or Soroban RPC (#1516).
type StatusHandler struct {
	switches  MoneyPathSwitchService
	freshness freshness.Reader
	pingRPC   func(ctx context.Context) error
	cacheTTL  time.Duration
	now       func() time.Time

	mu        sync.Mutex
	cached    *PublicStatusPayload
	expiresAt time.Time
}

func NewStatusHandler(
	switches MoneyPathSwitchService,
	freshnessReader freshness.Reader,
	pingRPC func(ctx context.Context) error,
) *StatusHandler {
	return &StatusHandler{
		switches:  switches,
		freshness: freshnessReader,
		pingRPC:   pingRPC,
		cacheTTL:  defaultStatusCacheTTL,
		now:       time.Now,
	}
}

func (h *StatusHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/status", h.GetStatus)
}

func (h *StatusHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	now := h.now()

	h.mu.Lock()
	if h.cached != nil && now.Before(h.expiresAt) {
		snap := *h.cached
		h.mu.Unlock()
		w.Header().Set("Cache-Control", "public, max-age=5")
		response.WriteJSON(w, http.StatusOK, response.OK(snap))
		return
	}
	h.mu.Unlock()

	payload := h.compute(r.Context(), now)

	h.mu.Lock()
	h.cached = &payload
	h.expiresAt = now.Add(h.cacheTTL)
	h.mu.Unlock()

	w.Header().Set("Cache-Control", "public, max-age=5")
	response.WriteJSON(w, http.StatusOK, response.OK(payload))
}

func (h *StatusHandler) compute(ctx context.Context, now time.Time) PublicStatusPayload {
	out := PublicStatusPayload{
		Status:     "ok",
		RPCHealthy: true,
		UpdatedAt:  now.UTC(),
	}

	if h.switches != nil {
		if list, err := h.switches.List(ctx); err == nil {
			for _, sw := range list {
				if !sw.Paused {
					continue
				}
				switch sw.Operation {
				case moneypath.OperationDeposit:
					out.DepositsPaused = true
					if out.PauseReason == "" && sw.Reason != "" {
						out.PauseReason = sw.Reason
					}
				case moneypath.OperationWithdrawal:
					out.WithdrawalsPaused = true
					if out.PauseReason == "" && sw.Reason != "" {
						out.PauseReason = sw.Reason
					}
				}
			}
		}
	}

	if h.freshness != nil {
		snap := h.freshness.Snapshot()
		if snap.Sampled {
			out.IndexerLagSeconds = int64(snap.Lag.Seconds())
			out.IndexerLagLedgers = snap.LagLedgers
			out.IndexerStale = snap.Stale || snap.Lag > 120*time.Second
		}
	}

	if h.pingRPC != nil {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := h.pingRPC(probeCtx); err != nil {
			out.RPCHealthy = false
		}
	}

	switch {
	case out.DepositsPaused || out.WithdrawalsPaused:
		out.Status = "paused"
	case out.IndexerStale || !out.RPCHealthy:
		out.Status = "degraded"
	default:
		out.Status = "ok"
	}

	return out
}
