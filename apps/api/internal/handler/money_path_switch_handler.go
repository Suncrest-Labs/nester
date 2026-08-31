package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/moneypath"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// MoneyPathSwitchService is the slice of the switch service this handler
// needs, declared here so tests can substitute one without a database.
type MoneyPathSwitchService interface {
	List(ctx context.Context) ([]moneypath.Switch, error)
	SetPaused(ctx context.Context, op moneypath.Operation, paused bool, reason string, actor *uuid.UUID, ipAddress string) (moneypath.Switch, error)
}

// MoneyPathSwitchHandler exposes the global deposit and withdrawal pause
// switches (nester#1120).
//
// Registered under /api/v1/admin/, which the production auth rules gate on
// the admin role, so no additional role check is needed here.
type MoneyPathSwitchHandler struct {
	service MoneyPathSwitchService
}

func NewMoneyPathSwitchHandler(svc MoneyPathSwitchService) *MoneyPathSwitchHandler {
	return &MoneyPathSwitchHandler{service: svc}
}

func (h *MoneyPathSwitchHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/money-path/switches", h.list)
	mux.HandleFunc("PUT /api/v1/admin/money-path/switches/{operation}", h.set)

	// Unauthenticated read of the pause state, so the dapp can explain a
	// pause honestly to a user who is not signed in — the acceptance
	// criterion asks the UI to say what is happening rather than showing a
	// generic error, and a signed-out visitor sees the same outage.
	//
	// It exposes only whether each operation is halted and the operator's
	// reason, which is information the paused response already carries.
	mux.HandleFunc("GET /api/v1/money-path/status", h.publicStatus)
}

type switchResponse struct {
	Operation string    `json:"operation"`
	Paused    bool      `json:"paused"`
	Reason    string    `json:"reason"`
	UpdatedAt time.Time `json:"updated_at"`
}

type setSwitchRequest struct {
	Paused *bool  `json:"paused"`
	Reason string `json:"reason"`
}

func toSwitchResponse(s moneypath.Switch) switchResponse {
	return switchResponse{
		Operation: string(s.Operation),
		Paused:    s.Paused,
		Reason:    s.Reason,
		UpdatedAt: s.UpdatedAt,
	}
}

func (h *MoneyPathSwitchHandler) list(w http.ResponseWriter, r *http.Request) {
	switches, err := h.service.List(r.Context())
	if err != nil {
		writeMoneyPathError(w, r, err)
		return
	}

	out := make([]switchResponse, 0, len(switches))
	for _, s := range switches {
		out = append(out, toSwitchResponse(s))
	}
	response.WriteJSON(w, http.StatusOK, response.OK(out))
}

func (h *MoneyPathSwitchHandler) set(w http.ResponseWriter, r *http.Request) {
	op := moneypath.Operation(r.PathValue("operation"))
	if !op.Valid() {
		response.WriteJSON(w, http.StatusBadRequest,
			response.ValidationErr("operation must be one of: deposit, withdrawal"))
		return
	}

	var req setSwitchRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}
	// Required rather than defaulted: a body that forgot the field would
	// otherwise silently release a switch someone engaged during an incident.
	if req.Paused == nil {
		response.WriteJSON(w, http.StatusBadRequest,
			response.ValidationErr("paused is required and must be true or false"))
		return
	}

	var actor *uuid.UUID
	if user, ok := auth.GetUserFromContext(r.Context()); ok {
		if id, err := uuid.Parse(user.ID); err == nil {
			actor = &id
		}
	}

	updated, err := h.service.SetPaused(r.Context(), op, *req.Paused, req.Reason, actor, clientIP(r))
	if err != nil {
		writeMoneyPathError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(toSwitchResponse(updated)))
}

func (h *MoneyPathSwitchHandler) publicStatus(w http.ResponseWriter, r *http.Request) {
	switches, err := h.service.List(r.Context())
	if err != nil {
		writeMoneyPathError(w, r, err)
		return
	}

	out := make([]switchResponse, 0, len(switches))
	for _, s := range switches {
		out = append(out, toSwitchResponse(s))
	}
	response.WriteJSON(w, http.StatusOK, response.OK(out))
}

func writeMoneyPathError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, moneypath.ErrUnknownOperation):
		response.WriteJSON(w, http.StatusBadRequest,
			response.ValidationErr("operation must be one of: deposit, withdrawal"))
	default:
		logpkg.FromContext(r.Context()).Error("money path switch handler failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError,
			response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
	}
}
