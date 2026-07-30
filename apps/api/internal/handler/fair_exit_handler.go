package handler

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

const defaultFairExitHistoryLimit = 100

// fairExitReader is the read-only projection built by the Stellar event
// indexer for the fair-ordering emergency queue (#814), penalty escrow
// (#805), and slippage-safe rebalance (#810) event streams. Satisfied by
// *postgres.FairExitRepository; defined here (rather than depending on the
// postgres package directly) so the handler only depends on the shape it
// needs.
type fairExitReader interface {
	ListEmergencyQueue(ctx context.Context, contractAddress string) ([]vault.EmergencyQueueEntry, error)
	ListPenaltyEvents(ctx context.Context, contractAddress string, limit int) ([]vault.PenaltyEvent, error)
	ListPenaltyDistributions(ctx context.Context, contractAddress string, limit int) ([]vault.PenaltyDistribution, error)
	ListRebalanceLegs(ctx context.Context, contractAddress string, limit int) ([]vault.RebalanceLeg, error)
	ListRebalanceCompletions(ctx context.Context, contractAddress string, limit int) ([]vault.RebalanceCompletion, error)
}

// FairExitHandler exposes read-only history for the on-chain fair-exit
// features. It reuses VaultHandler's vault service purely to resolve a
// vault id to its contract address and confirm the caller owns it — it
// does not write anything itself.
type FairExitHandler struct {
	service *service.VaultService
	reader  fairExitReader
}

func NewFairExitHandler(service *service.VaultService, reader fairExitReader) *FairExitHandler {
	return &FairExitHandler{service: service, reader: reader}
}

func (h *FairExitHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/vaults/{id}/emergency-queue", h.getEmergencyQueue)
	mux.HandleFunc("GET /api/v1/vaults/{id}/penalty-events", h.getPenaltyEvents)
	mux.HandleFunc("GET /api/v1/vaults/{id}/penalty-distributions", h.getPenaltyDistributions)
	mux.HandleFunc("GET /api/v1/vaults/{id}/rebalance-legs", h.getRebalanceLegs)
	mux.HandleFunc("GET /api/v1/vaults/{id}/rebalance-completions", h.getRebalanceCompletions)
}

// resolveOwnedVaultContract looks up the vault by path id, confirms it
// belongs to the authenticated caller, and returns its on-chain contract
// address. Writes an error response and returns ok=false on any failure.
func (h *FairExitHandler) resolveOwnedVaultContract(w http.ResponseWriter, r *http.Request) (contractAddress string, ok bool) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return "", false
	}

	user, authed := auth.GetUserFromContext(r.Context())
	if !authed {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return "", false
	}

	v, err := h.service.GetVault(r.Context(), vaultID)
	if err != nil {
		response.WriteJSON(w, http.StatusNotFound, response.Err(http.StatusNotFound, "NOT_FOUND", "vault not found"))
		return "", false
	}

	if v.UserID.String() != user.ID {
		response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", "forbidden"))
		return "", false
	}

	return v.ContractAddress, true
}

func (h *FairExitHandler) getEmergencyQueue(w http.ResponseWriter, r *http.Request) {
	contractAddress, ok := h.resolveOwnedVaultContract(w, r)
	if !ok {
		return
	}
	entries, err := h.reader.ListEmergencyQueue(r.Context(), contractAddress)
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL", "failed to load emergency queue"))
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(entries))
}

func (h *FairExitHandler) getPenaltyEvents(w http.ResponseWriter, r *http.Request) {
	contractAddress, ok := h.resolveOwnedVaultContract(w, r)
	if !ok {
		return
	}
	events, err := h.reader.ListPenaltyEvents(r.Context(), contractAddress, defaultFairExitHistoryLimit)
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL", "failed to load penalty events"))
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(events))
}

func (h *FairExitHandler) getPenaltyDistributions(w http.ResponseWriter, r *http.Request) {
	contractAddress, ok := h.resolveOwnedVaultContract(w, r)
	if !ok {
		return
	}
	dists, err := h.reader.ListPenaltyDistributions(r.Context(), contractAddress, defaultFairExitHistoryLimit)
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL", "failed to load penalty distributions"))
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(dists))
}

func (h *FairExitHandler) getRebalanceLegs(w http.ResponseWriter, r *http.Request) {
	contractAddress, ok := h.resolveOwnedVaultContract(w, r)
	if !ok {
		return
	}
	legs, err := h.reader.ListRebalanceLegs(r.Context(), contractAddress, defaultFairExitHistoryLimit)
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL", "failed to load rebalance legs"))
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(legs))
}

func (h *FairExitHandler) getRebalanceCompletions(w http.ResponseWriter, r *http.Request) {
	contractAddress, ok := h.resolveOwnedVaultContract(w, r)
	if !ok {
		return
	}
	completions, err := h.reader.ListRebalanceCompletions(r.Context(), contractAddress, defaultFairExitHistoryLimit)
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL", "failed to load rebalance completions"))
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(completions))
}
