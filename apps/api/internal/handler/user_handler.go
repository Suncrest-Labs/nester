package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
	"github.com/suncrestlabs/nester/apps/api/internal/objectstorage"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type UserHandler struct {
	service       *service.UserService
	userVaultsSvc *service.UserVaultsService
	validator     *validator.Validate
	kycStore      objectstorage.Store
}

func NewUserHandler(service *service.UserService) *UserHandler {
	return &UserHandler{
		service:   service,
		validator: validator.New(validator.WithRequiredStructEnabled()),
	}
}

// SetKYCStore wires the object store submitKYC uploads identity documents
// to. Left unset (nil) in an environment where object storage hasn't been
// provisioned yet — submitKYC treats that the same as "storage is not
// ready" and rejects the upload (503) rather than accepting and discarding
// it (nester#1191).
func (h *UserHandler) SetKYCStore(store objectstorage.Store) {
	h.kycStore = store
}

// SetUserVaultsService wires the intelligence-facing user vaults list.
func (h *UserHandler) SetUserVaultsService(svc *service.UserVaultsService) {
	h.userVaultsSvc = svc
}

type registerUserRequest struct {
	WalletAddress string `json:"wallet_address" validate:"required"`
	DisplayName   string `json:"display_name" validate:"required"`
}

func (h *UserHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/users", h.registerUser)
	mux.HandleFunc("GET /api/v1/users/wallet/{address}", h.getUserByWallet)
	mux.HandleFunc("POST /api/v1/users/kyc/{id}", h.submitKYC)
	mux.HandleFunc("GET /api/v1/users/kyc/{id}", h.getKYCStatus)
	mux.HandleFunc("POST /api/v1/users/me/kyc", h.submitKYCMe)
	mux.HandleFunc("GET /api/v1/users/me/kyc", h.getKYCStatusMe)
	mux.HandleFunc("GET /api/v1/users/profile", h.getProfile)
	mux.HandleFunc("PATCH /api/v1/users/profile", h.updateProfile)
	mux.HandleFunc("GET /api/v1/user-vaults/{id}", h.listUserVaultsForIntelligence)
	mux.HandleFunc("GET /api/v1/users/{id}", h.getUserByID)
}

func (h *UserHandler) registerUser(w http.ResponseWriter, r *http.Request) {
	var req registerUserRequest
	if err := h.decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	if err := h.validator.Struct(req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	model, err := h.service.RegisterUser(r.Context(), req.WalletAddress, req.DisplayName)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusCreated, response.Created(model))
}

func (h *UserHandler) getUserByID(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid user ID"))
		return
	}

	model, err := h.service.GetUser(r.Context(), id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(model))
}

func (h *UserHandler) listUserVaultsForIntelligence(w http.ResponseWriter, r *http.Request) {
	if h.userVaultsSvc == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "user vaults service not configured"))
		return
	}
	idStr := r.PathValue("id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid user ID"))
		return
	}
	if !h.authorizeUserAccess(w, r, userID) {
		return
	}
	result, err := h.userVaultsSvc.ListForIntelligence(r.Context(), userID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(result))
}

func (h *UserHandler) authorizeUserAccess(w http.ResponseWriter, r *http.Request, userID uuid.UUID) bool {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "authentication required"))
		return false
	}
	if user.ID != userID.String() {
		response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", "forbidden"))
		return false
	}
	return true
}

func (h *UserHandler) getProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	model, err := h.service.GetProfile(r.Context(), userID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(model))
}

type updateProfileRequest struct {
	RiskProfile         *string `json:"risk_profile"`
	SavingsGoal         *string `json:"savings_goal"`
	OnboardingCompleted *bool   `json:"onboarding_completed"`
}

func (h *UserHandler) updateProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	var req updateProfileRequest
	if err := h.decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}
	in := service.UpdateProfileInput{
		SavingsGoal:         req.SavingsGoal,
		OnboardingCompleted: req.OnboardingCompleted,
	}
	if req.RiskProfile != nil {
		rp := strings.ToLower(strings.TrimSpace(*req.RiskProfile))
		switch user.RiskProfile(rp) {
		case user.RiskProfileConservative, user.RiskProfileModerate, user.RiskProfileAggressive:
			profile := user.RiskProfile(rp)
			in.RiskProfile = &profile
		default:
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("risk_profile must be conservative, moderate, or aggressive"))
			return
		}
	}
	model, err := h.service.UpdateProfile(r.Context(), userID, in)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(model))
}

func (h *UserHandler) authenticatedUserID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	u, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "authentication required"))
		return uuid.Nil, false
	}
	id, err := uuid.Parse(u.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid user identity"))
		return uuid.Nil, false
	}
	return id, true
}

func (h *UserHandler) getUserByWallet(w http.ResponseWriter, r *http.Request) {
	address := r.PathValue("address")
	if address == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("wallet address is required"))
		return
	}

	model, err := h.service.GetUserByWallet(r.Context(), address)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(model))
}

const (
	// maxKYCUploadBytes caps the entire KYC submission body. Identity documents
	// are photographs; 12 MB accommodates a high-resolution scan with form
	// fields alongside it.
	maxKYCUploadBytes = 12 << 20
	// kycInMemoryBytes is how much of the multipart form is buffered in memory
	// before the remainder spills to temporary files.
	kycInMemoryBytes = 10 << 20
	// MaxKYCDocumentBytes bounds a single id_front/id_back file, distinct
	// from maxKYCUploadBytes which bounds the whole multipart body (both
	// files plus form fields together).
	MaxKYCDocumentBytes = 8 << 20
)

// KYCAllowedContentTypes are the only content types submitKYC will store —
// identity documents are photographs or scans, never an arbitrary file
// (nester#1191's third acceptance criterion).
var KYCAllowedContentTypes = []string{"image/jpeg", "image/png", "application/pdf"}

func (h *UserHandler) submitKYC(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid user ID"))
		return
	}
	// Ownership check (IDOR fix): only the authenticated user may submit KYC
	// documents for their own account.
	if !h.authorizeUserAccess(w, r, userID) {
		return
	}

	h.submitKYCFor(w, r, userID)
}

// submitKYCFor runs the KYC submission for an already-resolved user id.
// Shared by submitKYC (id from the path) and submitKYCMe (id from the
// session) so the two routes cannot drift — an earlier copy of this logic
// on the /users/me/ route still discarded the identity fields and wrote a
// mock storage key, reintroducing nester#1190 and nester#1191.
func (h *UserHandler) submitKYCFor(w http.ResponseWriter, r *http.Request, userID uuid.UUID) {

	if h.kycStore == nil {
		// Storage is not ready in this environment — reject the upload
		// rather than accept and discard it (nester#1191's explicit
		// guidance for this situation).
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "document storage is not available"))
		return
	}

	// ParseMultipartForm's argument bounds only how much is held in memory:
	// anything beyond it spills to temporary files, so on its own it does not
	// bound the request at all and a large upload can exhaust disk
	// (nester#1035, G120). MaxBytesReader caps the request body itself, which
	// is the actual limit.
	r.Body = http.MaxBytesReader(w, r.Body, maxKYCUploadBytes)
	if err := r.ParseMultipartForm(kycInMemoryBytes); err != nil { // #nosec G120 -- the request body is bounded by MaxBytesReader on the line above
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("could not parse multipart form"))
		return
	}

	fullName := strings.TrimSpace(r.FormValue("full_name"))
	dateOfBirthRaw := r.FormValue("date_of_birth")
	country := strings.ToUpper(strings.TrimSpace(r.FormValue("country")))
	idType := r.FormValue("id_type")
	idNumber := r.FormValue("id_number")

	if idType == "" || idNumber == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("id_type and id_number are required"))
		return
	}
	if fullName == "" {
		h.writeDomainError(w, r, user.ErrMissingIdentity)
		return
	}
	dateOfBirth, err := user.ParseDateOfBirth(dateOfBirthRaw)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	if !user.IsValidCountryCode(country) {
		h.writeDomainError(w, r, user.ErrInvalidCountry)
		return
	}
	identity := user.KYCIdentity{FullName: fullName, DateOfBirth: dateOfBirth, Country: country}

	idFrontFile, idFrontHeader, err := r.FormFile("id_front")
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("id_front is required"))
		return
	}
	defer idFrontFile.Close()

	frontContentType := idFrontHeader.Header.Get("Content-Type")
	frontKey, err := h.storeKYCDocument(r.Context(), userID, frontContentType, idFrontFile)
	if err != nil {
		h.writeKYCUploadError(w, r, err)
		return
	}

	var backKey *string
	idBackFile, idBackHeader, err := r.FormFile("id_back")
	if err == nil {
		defer idBackFile.Close()
		backContentType := idBackHeader.Header.Get("Content-Type")
		bk, err := h.storeKYCDocument(r.Context(), userID, backContentType, idBackFile)
		if err != nil {
			h.cleanupKYCDocuments(r.Context(), frontKey)
			h.writeKYCUploadError(w, r, err)
			return
		}
		backKey = &bk
	}

	if err := h.service.SubmitKYC(r.Context(), userID, identity, idType, idNumber, frontKey, backKey); err != nil {
		if backKey != nil {
			h.cleanupKYCDocuments(r.Context(), frontKey, *backKey)
		} else {
			h.cleanupKYCDocuments(r.Context(), frontKey)
		}
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusAccepted, response.OK(map[string]string{"status": "pending"}))
}

// storeKYCDocument uploads one id_front/id_back file to real object
// storage and returns the server-generated key. The client's filename is
// never read here at all — it plays no part in the generated key
// (nester#1191).
func (h *UserHandler) storeKYCDocument(ctx context.Context, userID uuid.UUID, contentType string, file multipart.File) (string, error) {
	limited := io.LimitReader(file, MaxKYCDocumentBytes+1)
	key, err := h.kycStore.Put(ctx, userID.String(), contentType, limited)
	if err != nil {
		return "", err
	}
	return key, nil
}

// cleanupKYCDocuments removes already-stored KYC documents on a later
// failure path. Store has no transaction to roll back a partial upload, so
// this is compensating cleanup: without it, an id_front (and/or id_back)
// object that was successfully persisted before a subsequent step failed
// (validating id_back, or SubmitKYC itself) would be orphaned — never
// referenced by any KYC record and never removed. Best-effort: a delete
// failure is logged, not surfaced, since the request is already failing for
// its own reason and the caller has nothing to fall back to.
func (h *UserHandler) cleanupKYCDocuments(ctx context.Context, keys ...string) {
	for _, key := range keys {
		if err := h.kycStore.Delete(ctx, key); err != nil {
			logpkg.FromContext(ctx).Error("failed to clean up orphaned kyc document", "key", key, "error", err.Error())
		}
	}
}

// writeKYCUploadError maps a storage-layer failure to an HTTP response. A
// failed upload fails the request rather than persisting a KYC record that
// points at nothing (nester#1191's fourth acceptance criterion) — the
// caller in submitKYC returns immediately after calling this and never
// reaches the SubmitKYC persistence call.
func (h *UserHandler) writeKYCUploadError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, objectstorage.ErrContentTypeNotAllowed):
		response.WriteJSON(w, http.StatusUnsupportedMediaType, response.ValidationErr(err.Error()))
	case errors.Is(err, objectstorage.ErrTooLarge):
		response.WriteJSON(w, http.StatusRequestEntityTooLarge, response.ValidationErr(err.Error()))
	default:
		logpkg.FromContext(r.Context()).Error("kyc document upload failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "UPLOAD_FAILED", "document upload failed"))
	}
}

func (h *UserHandler) getKYCStatus(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid user ID"))
		return
	}
	// Ownership check (IDOR fix): only the authenticated user may read their
	// own KYC status.
	if !h.authorizeUserAccess(w, r, userID) {
		return
	}

	model, err := h.service.GetUser(r.Context(), userID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	resp := map[string]any{
		"status": model.KYCStatus,
	}
	if model.KYCSubmittedAt != nil {
		resp["submitted_at"] = model.KYCSubmittedAt
	}
	if model.KYCReviewedAt != nil {
		resp["reviewed_at"] = model.KYCReviewedAt
	}
	if model.KYCRejectionReason != nil {
		resp["rejection_reason"] = model.KYCRejectionReason
	}

	response.WriteJSON(w, http.StatusOK, response.OK(resp))
}

// submitKYCMe submits KYC data for the authenticated user (no user ID param needed).
// submitKYCMe submits KYC for the authenticated user.
//
// Delegates to submitKYCFor rather than repeating the parsing and storage
// logic. The copy that used to live here discarded full_name, date_of_birth
// and country with `_ =`, and built its storage key by concatenating the
// client-supplied filename onto "s3://mock-bucket/" — reintroducing both
// nester#1190 and nester#1191 on a second route after they were fixed on the
// first.
func (h *UserHandler) submitKYCMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}

	h.submitKYCFor(w, r, userID)
}

// getKYCStatusMe retrieves KYC status for the authenticated user.
func (h *UserHandler) getKYCStatusMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}

	model, err := h.service.GetUser(r.Context(), userID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	resp := map[string]any{
		"status": model.KYCStatus,
	}
	if model.KYCSubmittedAt != nil {
		resp["submitted_at"] = model.KYCSubmittedAt
	}
	if model.KYCReviewedAt != nil {
		resp["reviewed_at"] = model.KYCReviewedAt
	}
	if model.KYCRejectionReason != nil {
		resp["rejection_reason"] = model.KYCRejectionReason
	}

	response.WriteJSON(w, http.StatusOK, response.OK(resp))
}

func (h *UserHandler) decodeJSON(r *http.Request, destination any) error {
	const maxBodyBytes = 1 << 20 // 1MB
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}

	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain only one JSON object")
	}

	return nil
}

func (h *UserHandler) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, user.ErrUserNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("user"))
	case errors.Is(err, user.ErrDuplicateWallet):
		response.WriteJSON(w, http.StatusConflict, response.Err(http.StatusConflict, "CONFLICT", err.Error()))
	case errors.Is(err, user.ErrInvalidWallet):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	case errors.Is(err, user.ErrInvalidDateOfBirth), errors.Is(err, user.ErrInvalidCountry), errors.Is(err, user.ErrMissingIdentity):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	default:
		logpkg.FromContext(r.Context()).Error("user handler failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
	}
}
