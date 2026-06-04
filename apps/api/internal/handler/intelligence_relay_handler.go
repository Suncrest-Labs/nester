package handler

import (
	"net/http"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// IntelligenceRelayHandler exposes POST /api/v1/intelligence/chat via the relay.
type IntelligenceRelayHandler struct {
	relay *service.RelayHandler
}

func NewIntelligenceRelayHandler(relay *service.RelayHandler) *IntelligenceRelayHandler {
	return &IntelligenceRelayHandler{relay: relay}
}

func (h *IntelligenceRelayHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/intelligence/chat", h.chat)
}

func (h *IntelligenceRelayHandler) chat(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, `{"success":false,"error":{"message":"unauthorized"}}`, http.StatusUnauthorized)
		return
	}
	ctx := service.WithViewer(r.Context(), service.Viewer{
		UserID:        user.ID,
		WalletAddress: user.WalletAddress,
	})
	h.relay.RelayChat(w, r.WithContext(ctx))
}
