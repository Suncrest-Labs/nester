package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	tvldom "github.com/suncrestlabs/nester/apps/api/internal/domain/tvl"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// TVLService is the read-side surface TVLHandler depends on.
type TVLService interface {
	GetVaultTVL(ctx context.Context, vaultID uuid.UUID) (tvldom.VaultTVL, error)
	GetAggregateTVL(ctx context.Context) (tvldom.AggregateTVL, error)
}

type TVLHandler struct {
	service TVLService
}

func NewTVLHandler(service TVLService) *TVLHandler {
	return &TVLHandler{service: service}
}

func (h *TVLHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/vaults/tvl", h.aggregate)
	mux.HandleFunc("GET /api/v1/vaults/{id}/tvl", h.byVault)
}

func (h *TVLHandler) aggregate(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.GetAggregateTVL(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(out))
}

func (h *TVLHandler) byVault(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	out, err := h.service.GetVaultTVL(r.Context(), vaultID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(out))
}

func (h *TVLHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, vault.ErrVaultNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("vault"))
	default:
		logpkg.FromContext(r.Context()).Error("tvl handler failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
	}
}
