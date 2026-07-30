package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/activity"
	"github.com/suncrestlabs/nester/apps/api/pkg/listquery"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// ActivityLister is the read side ActivityHandler depends on — satisfied by
// *service.ActivityService, kept as an interface so handler tests can stub
// it without a database.
type ActivityLister interface {
	List(ctx context.Context, userID uuid.UUID, filter activity.ListFilter) ([]activity.Item, string, string, error)
}

// ActivityHandler serves GET /api/v1/activity: the unified transaction-
// history feed the dApp's history page consumes.
type ActivityHandler struct {
	svc ActivityLister
}

func NewActivityHandler(svc ActivityLister) *ActivityHandler {
	return &ActivityHandler{svc: svc}
}

func (h *ActivityHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/activity", h.list)
}

// activityItemDTO matches the frontend's fixed Transaction contract
// (apps/dapp/frontend/app/dashboard/history/page.tsx) exactly — Title-Case
// type/status strings and a plain numeric amount, not the internal
// activity.Item shape or the shared response.Response envelope.
type activityItemDTO struct {
	ID        string  `json:"id"`
	Timestamp string  `json:"timestamp"`
	Type      string  `json:"type"`
	VaultName string  `json:"vaultName"`
	Amount    float64 `json:"amount"`
	Asset     string  `json:"asset"`
	Status    string  `json:"status"`
	TxHash    string  `json:"txHash,omitempty"`
}

// activityListResponse is a deliberate, documented exception to the shared
// response.Response envelope: it matches the frontend's pre-existing,
// already-coded ActivityResponse contract literally, since that contract is
// fixed and there's no reason to force a frontend rewrite to adopt it.
type activityListResponse struct {
	Data       []activityItemDTO `json:"data"`
	NextCursor string            `json:"nextCursor,omitempty"`
	PrevCursor string            `json:"prevCursor,omitempty"`
}

var activityTypeInternalToDisplay = map[activity.EventType]string{
	activity.EventDeposit:     "Deposit",
	activity.EventWithdrawal:  "Withdrawal",
	activity.EventRebalance:   "Rebalance",
	activity.EventSettlement:  "Settlement",
	activity.EventYieldEarned: "Yield Earned",
}

var activityStatusInternalToDisplay = map[activity.Status]string{
	activity.StatusCompleted: "Confirmed",
	activity.StatusPending:   "Pending",
	activity.StatusFailed:    "Failed",
}

func (h *ActivityHandler) list(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}
	userID, err := uuid.Parse(user.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid token subject"))
		return
	}

	params, err := listquery.ParseActivityList(r)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	types := make([]activity.EventType, len(params.Types))
	for i, t := range params.Types {
		types[i] = activity.EventType(t)
	}

	items, nextCursor, prevCursor, err := h.svc.List(r.Context(), userID, activity.ListFilter{
		Types:    types,
		Status:   activity.Status(params.Status),
		VaultID:  params.VaultID,
		From:     params.From,
		To:       params.To,
		Search:   params.Search,
		Cursor:   params.Cursor,
		Backward: params.Backward,
		Limit:    params.Limit,
	})
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	dtos := make([]activityItemDTO, len(items))
	for i, it := range items {
		typeLabel, knownType := activityTypeInternalToDisplay[it.Type]
		if !knownType {
			typeLabel = string(it.Type)
		}
		statusLabel, knownStatus := activityStatusInternalToDisplay[it.Status]
		if !knownStatus {
			statusLabel = string(it.Status)
		}
		amount, _ := it.Amount.Float64()
		dtos[i] = activityItemDTO{
			ID:        it.ID.String(),
			Timestamp: it.CreatedAt.UTC().Format(time.RFC3339),
			Type:      typeLabel,
			VaultName: it.VaultName,
			Amount:    amount,
			Asset:     it.Currency,
			Status:    statusLabel,
			TxHash:    it.Ref,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(activityListResponse{
		Data:       dtos,
		NextCursor: nextCursor,
		PrevCursor: prevCursor,
	})
}
