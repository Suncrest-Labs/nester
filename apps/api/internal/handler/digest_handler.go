package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/digest"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// DigestLedgerAssembler computes the deterministic raw ledger ingredients
// for a user's digest period (#859).
type DigestLedgerAssembler interface {
	Assemble(ctx context.Context, userID uuid.UUID, period digest.Period) (digest.LedgerSource, error)
}

// DigestHandler exposes the digest-ledger source (consumed by the
// intelligence service via the relay) and the cached "latest digest" the
// frontend reads for the insights card.
type DigestHandler struct {
	ledger DigestLedgerAssembler
	cache  digest.Repository
}

func NewDigestHandler(ledger DigestLedgerAssembler, cache digest.Repository) *DigestHandler {
	return &DigestHandler{ledger: ledger, cache: cache}
}

func (h *DigestHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/users/digest-ledger", h.getLedgerSource)
	mux.HandleFunc("GET /api/v1/users/digest/latest", h.getLatest)
}

func (h *DigestHandler) getLedgerSource(w http.ResponseWriter, r *http.Request) {
	userID, ok := digestUserID(w, r)
	if !ok {
		return
	}

	period := digest.Period(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("period"))))
	if period == "" {
		period = digest.PeriodMonthly
	}
	if !period.Valid() {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("period must be one of: weekly, monthly"))
		return
	}

	source, err := h.ledger.Assemble(r.Context(), userID, period)
	if err != nil {
		logpkg.FromContext(r.Context()).Error("digest ledger assembly failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(source))
}

func (h *DigestHandler) getLatest(w http.ResponseWriter, r *http.Request) {
	userID, ok := digestUserID(w, r)
	if !ok {
		return
	}

	cached, err := h.cache.GetLatest(r.Context(), userID)
	if err != nil {
		logpkg.FromContext(r.Context()).Error("digest latest lookup failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}
	if cached == nil {
		response.WriteJSON(w, http.StatusNotFound, response.Err(http.StatusNotFound, "NOT_FOUND", "no digest has been generated yet"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(cached))
}

func digestUserID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "authentication required"))
		return uuid.Nil, false
	}
	userID, err := uuid.Parse(user.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid user identity"))
		return uuid.Nil, false
	}
	return userID, true
}
