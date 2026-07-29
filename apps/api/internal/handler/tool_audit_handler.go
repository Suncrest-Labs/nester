package handler

import (
	"encoding/json"
	"net/http"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/toolaudit"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type ToolAuditHandler struct {
	svc *service.ToolAuditService
}

func NewToolAuditHandler(svc *service.ToolAuditService) *ToolAuditHandler {
	return &ToolAuditHandler{svc: svc}
}

func (h *ToolAuditHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/internal/intelligence/tool-audit", h.Record)
}

func (h *ToolAuditHandler) Record(w http.ResponseWriter, r *http.Request) {
	var input toolaudit.ToolInvocation
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid request body"))
		return
	}

	result, err := h.svc.Record(r.Context(), input)
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", err.Error()))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(result))
}
