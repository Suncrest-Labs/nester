package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// jobQueueAdminRepo is the narrow persistence port JobQueueAdminHandler needs.
type jobQueueAdminRepo interface {
	GetByID(ctx context.Context, id uuid.UUID) (jobqueue.Job, error)
	ListDead(ctx context.Context, limit, offset int) ([]jobqueue.Job, error)
	ManualRetry(ctx context.Context, id uuid.UUID, runAt time.Time) error
}

// JobQueueAdminHandler exposes admin-facing dead-letter inspection and manual
// retry for the durable job queue (#1329).
type JobQueueAdminHandler struct {
	repo jobQueueAdminRepo
}

// NewJobQueueAdminHandler constructs a JobQueueAdminHandler.
func NewJobQueueAdminHandler(repo jobQueueAdminRepo) *JobQueueAdminHandler {
	return &JobQueueAdminHandler{repo: repo}
}

// Register registers the routes on the given ServeMux. These fall under the
// /api/v1/admin/ prefix so they're gated to the "admin" role by the
// Authenticate middleware's route rules.
func (h *JobQueueAdminHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/jobs/dead", h.listDead)
	mux.HandleFunc("POST /api/v1/admin/jobs/{id}/retry", h.retry)
}

func (h *JobQueueAdminHandler) listDead(w http.ResponseWriter, r *http.Request) {
	limit := parseAdminIntQuery(r.URL.Query().Get("limit"), defaultAdminPerPage)
	if limit > maxAdminPerPage {
		limit = maxAdminPerPage
	}
	offset := 0
	if page := parseAdminIntQuery(r.URL.Query().Get("page"), defaultAdminPage); page > 1 {
		offset = (page - 1) * limit
	}

	jobs, err := h.repo.ListDead(r.Context(), limit, offset)
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list dead jobs"))
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(jobs))
}

func (h *JobQueueAdminHandler) retry(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid job ID"))
		return
	}

	if err := h.repo.ManualRetry(r.Context(), id, time.Now()); err != nil {
		if errors.Is(err, jobqueue.ErrNotFound) {
			response.WriteJSON(w, http.StatusNotFound, response.NotFound("dead job"))
			return
		}
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "failed to retry job"))
		return
	}

	job, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "job retried but failed to reload"))
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(job))
}
